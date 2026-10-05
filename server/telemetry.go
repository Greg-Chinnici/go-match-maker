package server

import (
	"slices"
	"sync"
	"time"

	"go-match-maker/matchmaking"
	"go-match-maker/sim"
)

const (
	historySize = 600 // one sample a second: ten minutes
	recentSize  = 15  // matches shown in the dashboard feed
)

// Info describes how the server was started, for the dashboard header.
type Info struct {
	Mode       string
	Lobby      int
	Teams      int
	RatingDiff float64
	Store      string
	LoadgenURL string // where the dashboard looks for the load generator; empty hides it
}

// Sample is one second of server activity. Pointer fields are nil when
// nothing happened that second to measure, so charts show a gap, not a zero.
type Sample struct {
	Time        int64     `json:"t"` // unix milliseconds
	Queued      int       `json:"queued"`
	Active      int       `json:"active"`
	Created     int       `json:"created"`  // matches formed this second
	Reported    int       `json:"reported"` // matches reported this second
	AvgWaitS    *float64  `json:"avg_wait_s"`
	OldestWaitS float64   `json:"oldest_wait_s"`
	TeamGap     *float64  `json:"team_gap"` // mean gap between the best and worst team average
	Sim         sim.Stats `json:"sim"`
}

// MatchEvent is a reported match for the dashboard feed.
type MatchEvent struct {
	ID    string
	Time  time.Time
	Teams []TeamOutcome // best placement first
}

type TeamOutcome struct {
	Place   int // glicko.Unplaced (0) when the report left it out
	Players int
	Before  float64
	After   float64
}

// Telemetry collects per-second samples and recent matches and fans each new
// sample out to subscribers (the /events streams). It is safe for concurrent use.
type Telemetry struct {
	mu      sync.Mutex
	info    Info
	history []Sample
	recent  []MatchEvent // newest first

	// accumulated since the last sample
	created, reported int
	waitTotal         time.Duration
	waitCount         int
	gapTotal          float64
	gapCount          int

	subs map[chan Sample]struct{}
}

func NewTelemetry(info Info) *Telemetry {
	return &Telemetry{info: info, subs: make(map[chan Sample]struct{})}
}

var telemetry = NewTelemetry(Info{})

// StartTelemetry samples the queue and SimStats once a second for the
// dashboard.
func StartTelemetry(queue *matchmaking.Queue, info Info) {
	telemetry.mu.Lock()
	telemetry.info = info
	telemetry.mu.Unlock()

	go func() {
		for now := range time.Tick(time.Second) {
			telemetry.Sample(now, queue.Stats(), SimStats.Stats())
		}
	}()
}

// RecordMatchesCreated notes newly formed matches for the next sample.
func RecordMatchesCreated(matches []*matchmaking.ActiveMatch) {
	telemetry.RecordCreated(matches)
}

func (t *Telemetry) Info() Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.info
}

func (t *Telemetry) RecordCreated(matches []*matchmaking.ActiveMatch) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, m := range matches {
		t.created++
		t.waitTotal += m.AvgWait
		t.waitCount++

		if len(m.Teams) > 1 {
			lo, hi := m.Teams[0].AverageRating(), m.Teams[0].AverageRating()
			for _, team := range m.Teams[1:] {
				lo, hi = min(lo, team.AverageRating()), max(hi, team.AverageRating())
			}
			t.gapTotal += hi - lo
			t.gapCount++
		}
	}
}

func (t *Telemetry) RecordReported(ev MatchEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.reported++
	t.recent = slices.Insert(t.recent, 0, ev)
	if len(t.recent) > recentSize {
		t.recent = t.recent[:recentSize]
	}
}

// Sample closes the current interval, stores it, and sends it to subscribers.
func (t *Telemetry) Sample(now time.Time, q matchmaking.QueueStats, stats sim.Stats) Sample {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := Sample{
		Time:        now.UnixMilli(),
		Queued:      q.Queued,
		Active:      q.Active,
		Created:     t.created,
		Reported:    t.reported,
		OldestWaitS: q.OldestWait.Seconds(),
		Sim:         stats,
	}
	if t.waitCount > 0 {
		avg := (t.waitTotal / time.Duration(t.waitCount)).Seconds()
		s.AvgWaitS = &avg
	}
	if t.gapCount > 0 {
		gap := t.gapTotal / float64(t.gapCount)
		s.TeamGap = &gap
	}
	t.created, t.reported, t.waitTotal, t.waitCount, t.gapTotal, t.gapCount = 0, 0, 0, 0, 0, 0

	t.history = append(t.history, s)
	if len(t.history) > historySize {
		t.history = slices.Delete(t.history, 0, len(t.history)-historySize)
	}

	for ch := range t.subs {
		select {
		case ch <- s:
		default: // a slow client misses a sample rather than stalling the rest
		}
	}
	return s
}

func (t *Telemetry) History() []Sample {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.history)
}

// Latest is the most recent sample, or a zero Sample before the first one.
func (t *Telemetry) Latest() Sample {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.history) == 0 {
		return Sample{}
	}
	return t.history[len(t.history)-1]
}

func (t *Telemetry) Recent() []MatchEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.recent)
}

// Subscribe returns a channel of new samples and a function to stop.
func (t *Telemetry) Subscribe() (<-chan Sample, func()) {
	ch := make(chan Sample, 4)

	t.mu.Lock()
	t.subs[ch] = struct{}{}
	t.mu.Unlock()

	return ch, func() {
		t.mu.Lock()
		delete(t.subs, ch)
		t.mu.Unlock()
	}
}
