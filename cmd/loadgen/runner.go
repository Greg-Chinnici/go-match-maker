package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-match-maker/sim"
)

const (
	maxInFlight   = 256   // concurrent queue requests
	latencyWindow = 10000 // recent requests used for percentiles
)

var ErrRunning = errors.New("a run is already in progress")

// Config is one load run: Players simulated players (sim.PlayerID 0..n-1)
// queued round-robin at Rate requests per second for Duration.
type Config struct {
	Players  int
	Rate     float64
	Duration time.Duration
}

func (c Config) Validate() error {
	switch {
	case c.Players < 1 || c.Players > 100_000:
		return fmt.Errorf("players must be 1–100,000, got %d", c.Players)
	case c.Rate <= 0 || c.Rate > 20_000:
		return fmt.Errorf("rate must be above 0 and at most 20,000 per second, got %g", c.Rate)
	case c.Duration < time.Second || c.Duration > 2*time.Hour:
		return fmt.Errorf("duration must be 1s–2h, got %s", c.Duration)
	}
	return nil
}

// Status is a snapshot of the current or most recent run.
type Status struct {
	State     string  `json:"state"` // idle, running, done, stopped
	Target    string  `json:"target"`
	Players   int     `json:"players"`
	Rate      float64 `json:"rate"`
	Duration  string  `json:"duration"`
	ElapsedS  float64 `json:"elapsed_s"`
	Sent      int64   `json:"sent"`
	OK        int64   `json:"ok"`
	Busy      int64   `json:"busy"` // 409: player still queued or in a match
	Failed    int64   `json:"failed"`
	RPS       float64 `json:"rps"` // achieved, over the whole run
	P50Ms     float64 `json:"p50_ms"`
	P95Ms     float64 `json:"p95_ms"`
	P99Ms     float64 `json:"p99_ms"`
	LastError string  `json:"last_error,omitempty"`
}

// Runner runs one load run at a time against a match maker server.
type Runner struct {
	target string
	client *http.Client

	mu  sync.Mutex
	cur *run
}

func NewRunner(target string) *Runner {
	return &Runner{
		target: strings.TrimRight(target, "/"),
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        maxInFlight,
				MaxIdleConnsPerHost: maxInFlight,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

type run struct {
	cfg    Config
	start  time.Time
	cancel context.CancelFunc
	done   chan struct{}

	ok, busy, failed atomic.Int64

	mu        sync.Mutex
	state     string
	end       time.Time
	lastErr   string
	latencies []float64 // ring of recent latencies in ms
	latNext   int
}

// Start begins a run in the background.
func (r *Runner) Start(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur != nil && r.cur.running() {
		return ErrRunning
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	rn := &run{cfg: cfg, start: time.Now(), cancel: cancel, done: make(chan struct{}), state: "running"}
	r.cur = rn
	go rn.loop(ctx, r)
	return nil
}

// Stop ends the current run, if any, and waits for in-flight requests.
func (r *Runner) Stop() {
	r.mu.Lock()
	rn := r.cur
	r.mu.Unlock()
	if rn == nil {
		return
	}
	rn.cancel()
	<-rn.done
}

// Wait blocks until the current run, if any, finishes.
func (r *Runner) Wait() {
	r.mu.Lock()
	rn := r.cur
	r.mu.Unlock()
	if rn != nil {
		<-rn.done
	}
}

func (r *Runner) Status() Status {
	r.mu.Lock()
	rn := r.cur
	r.mu.Unlock()
	if rn == nil {
		return Status{State: "idle", Target: r.target}
	}
	return rn.status(r.target)
}

func (rn *run) running() bool {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.state == "running"
}

// loop schedules requests every 10ms to keep pace with the target rate.
func (rn *run) loop(ctx context.Context, r *Runner) {
	defer close(rn.done)

	sem := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	scheduled := 0
	schedule := func(now time.Time) bool {
		due := int(rn.cfg.Rate*now.Sub(rn.start).Seconds()) - scheduled
		for range due {
			select {
			case sem <- struct{}{}: // blocks when the server can't keep up
			case <-ctx.Done():
				return false
			}
			id := sim.PlayerID(scheduled % rn.cfg.Players)
			scheduled++
			wg.Go(func() {
				defer func() { <-sem }()
				rn.send(ctx, r, id)
			})
		}
		return true
	}

	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
		case now := <-ticker.C:
			running = schedule(now)
		}
	}
	wg.Wait()

	rn.mu.Lock()
	rn.end = time.Now()
	rn.state = "stopped"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		rn.state = "done"
	}
	rn.mu.Unlock()
}

func (rn *run) send(ctx context.Context, r *Runner, id string) {
	body := strings.NewReader(`{"uid":"` + id + `"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.target+"/queue", body)
	if err != nil {
		rn.fail(err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := r.client.Do(req)
	if err != nil {
		if ctx.Err() == nil { // requests cut off by the end of the run don't count
			rn.fail(err.Error())
		}
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	rn.recordLatency(time.Since(start))

	switch resp.StatusCode {
	case http.StatusOK:
		rn.ok.Add(1)
	case http.StatusConflict:
		rn.busy.Add(1)
	default:
		rn.fail("queue: " + resp.Status)
	}
}

func (rn *run) fail(msg string) {
	rn.failed.Add(1)
	rn.mu.Lock()
	rn.lastErr = msg
	rn.mu.Unlock()
}

func (rn *run) recordLatency(d time.Duration) {
	ms := float64(d.Microseconds()) / 1000
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if len(rn.latencies) < latencyWindow {
		rn.latencies = append(rn.latencies, ms)
		return
	}
	rn.latencies[rn.latNext] = ms
	rn.latNext = (rn.latNext + 1) % latencyWindow
}

func (rn *run) status(target string) Status {
	rn.mu.Lock()
	state, end, lastErr := rn.state, rn.end, rn.lastErr
	lat := slices.Clone(rn.latencies)
	rn.mu.Unlock()

	if end.IsZero() {
		end = time.Now()
	}
	elapsed := end.Sub(rn.start).Seconds()

	s := Status{
		State:     state,
		Target:    target,
		Players:   rn.cfg.Players,
		Rate:      rn.cfg.Rate,
		Duration:  formatDuration(rn.cfg.Duration),
		ElapsedS:  elapsed,
		OK:        rn.ok.Load(),
		Busy:      rn.busy.Load(),
		Failed:    rn.failed.Load(),
		LastError: lastErr,
	}
	s.Sent = s.OK + s.Busy + s.Failed
	if elapsed > 0 {
		s.RPS = float64(s.Sent) / elapsed
	}
	if len(lat) > 0 {
		slices.Sort(lat)
		pct := func(p float64) float64 { return lat[min(len(lat)-1, int(p*float64(len(lat))))] }
		s.P50Ms, s.P95Ms, s.P99Ms = pct(0.50), pct(0.95), pct(0.99)
	}
	return s
}
