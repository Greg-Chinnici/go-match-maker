package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"go-match-maker/sim"
)

// fakeQueue accepts each player once and answers 409 after, like a server
// whose matches never finish.
type fakeQueue struct {
	mu     sync.Mutex
	seen   map[string]int
	status int // when set, every request gets this status
}

func newFakeQueue(t *testing.T) (*fakeQueue, *httptest.Server) {
	f := &fakeQueue{seen: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ UID string }
		if r.URL.Path != "/queue" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.seen[req.UID]++
		n, status := f.seen[req.UID], f.status
		f.mu.Unlock()

		switch {
		case status != 0:
			w.WriteHeader(status)
		case n > 1:
			w.WriteHeader(http.StatusConflict)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		cfg Config
		ok  bool
	}{
		{Config{Players: 500, Rate: 100, Duration: time.Minute}, true},
		{Config{Players: 0, Rate: 100, Duration: time.Minute}, false},
		{Config{Players: 500, Rate: 0, Duration: time.Minute}, false},
		{Config{Players: 500, Rate: 50_000, Duration: time.Minute}, false},
		{Config{Players: 500, Rate: 100, Duration: 10 * time.Millisecond}, false},
		{Config{Players: 500, Rate: 100, Duration: 3 * time.Hour}, false},
	}
	for _, tt := range tests {
		if err := tt.cfg.Validate(); (err == nil) != tt.ok {
			t.Errorf("Validate(%+v) = %v, want ok=%v", tt.cfg, err, tt.ok)
		}
	}
}

func TestRunnerQueuesPlayersRoundRobin(t *testing.T) {
	fake, srv := newFakeQueue(t)
	runner := NewRunner(srv.URL)

	if err := runner.Start(Config{Players: 5, Rate: 200, Duration: time.Second}); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	s := runner.Status()
	if s.State != "done" {
		t.Errorf("state %q, want done", s.State)
	}
	if s.OK != 5 || s.Busy != s.Sent-5 || s.Failed != 0 {
		t.Errorf("status = %+v, want 5 ok and the rest busy", s)
	}
	if s.Sent < 100 || s.Sent > 220 {
		t.Errorf("sent %d in 1s at 200/s", s.Sent)
	}
	if s.P50Ms <= 0 || s.P99Ms < s.P50Ms {
		t.Errorf("latency p50 %.2f p99 %.2f", s.P50Ms, s.P99Ms)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	for n := range 5 {
		if fake.seen[sim.PlayerID(n)] == 0 {
			t.Errorf("player %d never queued", n)
		}
	}
	if len(fake.seen) != 5 {
		t.Errorf("%d distinct players queued, want 5", len(fake.seen))
	}
}

func TestRunnerStopAndRestart(t *testing.T) {
	_, srv := newFakeQueue(t)
	runner := NewRunner(srv.URL)

	if err := runner.Start(Config{Players: 10, Rate: 50, Duration: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(Config{Players: 10, Rate: 50, Duration: time.Hour}); err != ErrRunning {
		t.Errorf("second start = %v, want ErrRunning", err)
	}

	time.Sleep(100 * time.Millisecond)
	runner.Stop()
	if s := runner.Status(); s.State != "stopped" || s.Sent == 0 {
		t.Errorf("after stop: %+v", s)
	}

	if err := runner.Start(Config{Players: 10, Rate: 50, Duration: time.Second}); err != nil {
		t.Errorf("start after stop: %v", err)
	}
	runner.Stop()
}

func TestRunnerCountsFailures(t *testing.T) {
	fake, srv := newFakeQueue(t)
	fake.status = http.StatusInternalServerError
	runner := NewRunner(srv.URL)

	runner.Start(Config{Players: 3, Rate: 100, Duration: time.Second})
	runner.Wait()

	s := runner.Status()
	if s.Failed == 0 || s.OK != 0 || !strings.Contains(s.LastError, "500") {
		t.Errorf("status = %+v, want failures with a 500 error", s)
	}
}

const dashboard = "http://localhost:8080"

func newTestHandler(t *testing.T) (*Runner, http.Handler) {
	t.Helper()
	_, srv := newFakeQueue(t)
	runner := NewRunner(srv.URL)
	t.Cleanup(runner.Stop)
	return runner, newHandler(runner, []string{dashboard})
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestJSONAPI(t *testing.T) {
	runner, h := newTestHandler(t)
	post := func(body string) int {
		return serve(h, httptest.NewRequest("POST", "/run", strings.NewReader(body))).Code
	}

	if code := post(`{"players": 10, "rate": 50, "duration": "1h"}`); code != http.StatusAccepted {
		t.Fatalf("start: status %d", code)
	}
	if code := post(`{"players": 10, "rate": 50, "duration": "1h"}`); code != http.StatusConflict {
		t.Errorf("start while running: status %d, want 409", code)
	}

	var s Status
	json.NewDecoder(serve(h, httptest.NewRequest("GET", "/run", nil)).Body).Decode(&s)
	if s.State != "running" || s.Players != 10 {
		t.Errorf("GET /run = %+v", s)
	}

	serve(h, httptest.NewRequest("DELETE", "/run", nil))
	if got := runner.Status().State; got != "stopped" {
		t.Errorf("after DELETE: state %q", got)
	}

	for _, body := range []string{`{"players": 10, "rate": 50, "duration": "soon"}`, `{"players": -1, "rate": 50, "duration": "1m"}`, `nope`} {
		if code := post(body); code != http.StatusBadRequest {
			t.Errorf("POST %s: status %d, want 400", body, code)
		}
	}
}

func TestCORS(t *testing.T) {
	runner, h := newTestHandler(t)

	preflight := httptest.NewRequest("OPTIONS", "/panel/start", nil)
	preflight.Header.Set("Origin", dashboard)
	rec := serve(h, preflight)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != dashboard {
		t.Errorf("allowed preflight: %d %v", rec.Code, rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "HX-Request") {
		t.Errorf("preflight doesn't allow htmx headers: %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}

	preflight.Header.Set("Origin", "https://evil.example")
	if rec := serve(h, preflight); rec.Code != http.StatusForbidden {
		t.Errorf("foreign preflight: status %d, want 403", rec.Code)
	}

	// a plain cross-site form POST skips the preflight, so it must be refused
	form := httptest.NewRequest("POST", "/panel/start", strings.NewReader("players=10&rate=50&duration=1h"))
	form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form.Header.Set("Origin", "https://evil.example")
	if rec := serve(h, form); rec.Code != http.StatusForbidden {
		t.Errorf("foreign form POST: status %d, want 403", rec.Code)
	}
	if got := runner.Status().State; got != "idle" {
		t.Errorf("foreign POST started a run (state %q)", got)
	}

	get := httptest.NewRequest("GET", "/run", nil)
	get.Header.Set("Origin", "https://evil.example")
	if rec := serve(h, get); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("foreign GET got an Access-Control-Allow-Origin header")
	}
}

func panelDoc(t *testing.T, rec *httptest.ResponseRecorder) *goquery.Document {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	doc, err := goquery.NewDocumentFromReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestPanel(t *testing.T) {
	runner, h := newTestHandler(t)

	req := httptest.NewRequest("GET", "/panel", nil)
	req.Host = "localhost:9090"
	req.Header.Set("Origin", dashboard)
	rec := serve(h, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != dashboard {
		t.Error("panel response missing CORS header for the dashboard")
	}
	doc := panelDoc(t, rec)

	// the panel lives in the dashboard's page, so every URL must be absolute
	if post, _ := doc.Find("form").Attr("hx-post"); post != "http://localhost:9090/panel/start" {
		t.Errorf("form hx-post = %q", post)
	}
	if get, _ := doc.Find("#lg-status").Attr("hx-get"); get != "http://localhost:9090/panel/status" {
		t.Errorf("status hx-get = %q", get)
	}
	for _, name := range []string{"players", "rate", "duration"} {
		if doc.Find(`input[name="`+name+`"]`).Length() != 1 {
			t.Errorf("no %s input", name)
		}
	}

	start := func(form url.Values) *goquery.Document {
		req := httptest.NewRequest("POST", "/panel/start", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", dashboard)
		return panelDoc(t, serve(h, req))
	}

	bad := start(url.Values{"players": {"10"}, "rate": {"50"}, "duration": {"forever"}})
	if bad.Find(".lg-error").Length() == 0 {
		t.Error("invalid form shows no error")
	}

	good := start(url.Values{"players": {"10"}, "rate": {"50"}, "duration": {"1h"}})
	if good.Find(".lg-error").Length() != 0 || runner.Status().State != "running" {
		t.Errorf("valid form: error %q, state %q", good.Find(".lg-error").Text(), runner.Status().State)
	}
	if stop, _ := good.Find("button").Attr("hx-post"); !strings.HasSuffix(stop, "/panel/stop") {
		t.Errorf("running status has no stop button (hx-post %q)", stop)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Minute: "1m", 90 * time.Second: "1m30s", time.Hour: "1h", 2*time.Hour + 30*time.Minute: "2h30m", 45 * time.Second: "45s",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%s) = %q, want %q", d, got, want)
		}
	}
}
