package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"go-match-maker/glicko"
	"go-match-maker/matchmaking"
	"go-match-maker/sim"
)

// useTelemetry swaps in fresh telemetry and sim stats for one test.
func useTelemetry(t *testing.T, info Info) *Telemetry {
	t.Helper()
	oldT, oldS := telemetry, SimStats
	telemetry, SimStats = NewTelemetry(info), sim.NewTracker()
	t.Cleanup(func() { telemetry, SimStats = oldT, oldS })
	return telemetry
}

func team(ratings ...float64) matchmaking.Team {
	tm := matchmaking.Team{ID: "t"}
	for _, r := range ratings {
		tm.Players = append(tm.Players, glicko.EstablishedPlayer(r, 100, 0.06, ""))
	}
	return tm
}

func TestTelemetrySamples(t *testing.T) {
	tel := NewTelemetry(Info{})
	tel.RecordCreated([]*matchmaking.ActiveMatch{
		{Teams: []matchmaking.Team{team(1500, 1600), team(1500, 1500)}, AvgWait: 2 * time.Second}, // gap 50
		{Teams: []matchmaking.Team{team(1400), team(1550), team(1500)}, AvgWait: 4 * time.Second}, // gap 150
	})
	tel.RecordReported(MatchEvent{ID: "m1"})

	s := tel.Sample(time.UnixMilli(1000), matchmaking.QueueStats{Queued: 7, Active: 2, OldestWait: 5 * time.Second}, sim.Stats{})
	if s.Created != 2 || s.Reported != 1 || s.Queued != 7 || s.Active != 2 || s.OldestWaitS != 5 {
		t.Errorf("sample = %+v", s)
	}
	if s.AvgWaitS == nil || *s.AvgWaitS != 3 {
		t.Errorf("avg wait = %v, want 3s", s.AvgWaitS)
	}
	if s.TeamGap == nil || *s.TeamGap != 100 {
		t.Errorf("team gap = %v, want 100", s.TeamGap)
	}

	// a quiet second resets the counters and leaves gaps instead of zeros
	quiet := tel.Sample(time.UnixMilli(2000), matchmaking.QueueStats{}, sim.Stats{})
	if quiet.Created != 0 || quiet.Reported != 0 || quiet.AvgWaitS != nil || quiet.TeamGap != nil {
		t.Errorf("quiet sample = %+v", quiet)
	}

	if got := tel.Latest(); got.Time != 2000 {
		t.Errorf("latest sample time %d, want 2000", got.Time)
	}
}

func TestTelemetryKeepsTenMinutes(t *testing.T) {
	tel := NewTelemetry(Info{})
	for i := range historySize + 5 {
		tel.Sample(time.UnixMilli(int64(i)), matchmaking.QueueStats{}, sim.Stats{})
	}
	history := tel.History()
	if len(history) != historySize || history[0].Time != 5 {
		t.Errorf("history has %d samples starting at %d, want %d starting at 5", len(history), history[0].Time, historySize)
	}

	for i := range recentSize + 3 {
		tel.RecordReported(MatchEvent{ID: string(rune('a' + i))})
	}
	if recent := tel.Recent(); len(recent) != recentSize || recent[0].ID != string(rune('a'+recentSize+2)) {
		t.Errorf("recent has %d matches, newest %q", len(recent), recent[0].ID)
	}
}

func TestTelemetrySubscribe(t *testing.T) {
	tel := NewTelemetry(Info{})
	ch, stop := tel.Subscribe()

	tel.Sample(time.UnixMilli(42), matchmaking.QueueStats{Queued: 3}, sim.Stats{})
	select {
	case s := <-ch:
		if s.Queued != 3 {
			t.Errorf("got sample %+v", s)
		}
	case <-time.After(time.Second):
		t.Fatal("no sample delivered")
	}

	stop()
	tel.Sample(time.UnixMilli(43), matchmaking.QueueStats{}, sim.Stats{})
	select {
	case s := <-ch:
		t.Errorf("sample %+v delivered after unsubscribe", s)
	default:
	}
}

func TestReportRecordsMatchEvent(t *testing.T) {
	useFakeStore(t)
	tel := useTelemetry(t, Info{})
	q := matchmaking.NewQueue()
	mux := newMux(q)
	m := matchOf(t, q, mux, "BR", 6, 3, "a", "b", "c", "d", "e", "f")

	// only the winner placed: the other two teams are unplaced and listed last
	winner := m.Teams[2].ID
	if rec := do(t, mux, "POST", "/report", ReportRequest{MatchID: m.ID, Winner: winner}); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	recent := tel.Recent()
	if len(recent) != 1 || recent[0].ID != m.ID {
		t.Fatalf("recent = %+v, want match %s", recent, m.ID)
	}
	ev := recent[0]
	if len(ev.Teams) != 3 || ev.Teams[0].Place != 1 || ev.Teams[1].Place != glicko.Unplaced {
		t.Fatalf("teams = %+v, want the winner first", ev.Teams)
	}
	if ev.Teams[0].After <= ev.Teams[0].Before || ev.Teams[1].After >= ev.Teams[1].Before {
		t.Errorf("winner %+v should gain and losers %+v lose", ev.Teams[0], ev.Teams[1])
	}
}

func page(t *testing.T, mux http.Handler, path string) *goquery.Document {
	t.Helper()
	rec := do(t, mux, "GET", path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, rec.Code, rec.Body)
	}
	doc, err := goquery.NewDocumentFromReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestDashboardPage(t *testing.T) {
	useTelemetry(t, Info{Mode: "TDM", Lobby: 4, Teams: 2, RatingDiff: 200, Store: "memory", LoadgenURL: "http://localhost:9090"})
	mux := newMux(matchmaking.NewQueue())
	doc := page(t, mux, "/")

	if got, _ := doc.Find("body").Attr("sse-connect"); got != "/events" {
		t.Errorf("body sse-connect = %q, want /events", got)
	}
	if info := doc.Find(".info").Text(); !strings.Contains(info, "TDM") || !strings.Contains(info, "memory store") {
		t.Errorf("header info %q missing mode or store", info)
	}

	kpis := doc.Find("#kpis")
	if swap, _ := kpis.Attr("sse-swap"); swap != "kpis" {
		t.Errorf("#kpis sse-swap = %q", swap)
	}
	if n := kpis.Find(".tile").Length(); n != 6 {
		t.Errorf("%d stat tiles, want 6", n)
	}
	if swap, _ := doc.Find("#matches").Attr("sse-swap"); swap != "matches" {
		t.Errorf("#matches sse-swap = %q", swap)
	}

	canvases := doc.Find("canvas[data-chart]")
	if canvases.Length() != len(trafficCharts)+len(modelCharts) {
		t.Errorf("%d time-series charts, want %d", canvases.Length(), len(trafficCharts)+len(modelCharts))
	}
	canvases.Each(func(_ int, c *goquery.Selection) {
		var spec struct {
			ID     string                 `json:"id"`
			Series []struct{ Key string } `json:"series"`
		}
		raw, _ := c.Attr("data-chart")
		if err := json.Unmarshal([]byte(raw), &spec); err != nil || len(spec.Series) == 0 {
			t.Errorf("data-chart %q: %v", raw, err)
		}
	})
	for _, id := range []string{"scatter", "histogram"} {
		if doc.Find("canvas#"+id).Length() != 1 {
			t.Errorf("no %s canvas", id)
		}
	}

	// every chart has a table view
	tables := doc.Find("details.tbl")
	if tables.Length() != canvases.Length()+2 {
		t.Errorf("%d table views for %d charts", tables.Length(), canvases.Length()+2)
	}
	tables.Each(func(_ int, d *goquery.Selection) {
		if get, _ := d.Attr("hx-get"); !strings.HasPrefix(get, "/dashboard/table/") {
			t.Errorf("table view hx-get = %q", get)
		}
	})

	if get, _ := doc.Find("#loadgen").Attr("hx-get"); get != "http://localhost:9090/panel" {
		t.Errorf("#loadgen hx-get = %q", get)
	}
}

func TestDashboardHidesLoadgenWhenDisabled(t *testing.T) {
	useTelemetry(t, Info{Mode: "FFA"})
	doc := page(t, newMux(matchmaking.NewQueue()), "/")
	if doc.Find("#loadgen").Length() != 0 {
		t.Error("load generator panel shown with no loadgen URL")
	}
}

func TestDashboardTables(t *testing.T) {
	tel := useTelemetry(t, Info{})
	mux := newMux(matchmaking.NewQueue())

	for i := range 3 {
		tel.RecordCreated([]*matchmaking.ActiveMatch{{Teams: []matchmaking.Team{team(1500), team(1600)}}})
		tel.Sample(time.UnixMilli(int64(i)*1000), matchmaking.QueueStats{Queued: 10 + i}, sim.Stats{})
	}
	doc := page(t, mux, "/dashboard/table/queue")
	if rows := doc.Find("tbody tr"); rows.Length() != 3 || rows.First().Find("td").Last().Text() != "12" {
		t.Errorf("queue table: %d rows, newest %q", rows.Length(), rows.First().Find("td").Last().Text())
	}
	if headers := doc.Find("th").Length(); headers != 2 {
		t.Errorf("queue table has %d columns, want Time + Waiting", headers)
	}

	SimStats.RecordMatch(true, map[string]float64{"a": 1500, "b": 1700})
	if rows := page(t, mux, "/dashboard/table/scatter").Find("tbody tr").Length(); rows != 2 {
		t.Errorf("scatter table has %d rows, want 2", rows)
	}
	if rows := page(t, mux, "/dashboard/table/histogram").Find("tbody tr").Length(); rows == 0 {
		t.Error("histogram table is empty")
	}

	if rec := do(t, mux, "GET", "/dashboard/table/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown chart: status %d, want 404", rec.Code)
	}
	if rec := do(t, mux, "GET", "/static/app.js", nil); rec.Code != http.StatusOK {
		t.Errorf("GET /static/app.js: status %d", rec.Code)
	}
}

type sseEvent struct{ name, data string }

// readEvents parses server-sent events from a stream onto a channel.
func readEvents(body *bufio.Scanner, out chan<- sseEvent) {
	var ev sseEvent
	for body.Scan() {
		line := body.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			ev.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if ev.data != "" {
				ev.data += "\n"
			}
			ev.data += strings.TrimPrefix(line, "data: ")
		case line == "":
			out <- ev
			ev = sseEvent{}
		}
	}
	close(out)
}

func TestEventsStream(t *testing.T) {
	tel := useTelemetry(t, Info{})
	srv := httptest.NewServer(newMux(matchmaking.NewQueue()))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}

	events := make(chan sseEvent)
	go readEvents(bufio.NewScanner(resp.Body), events)
	next := func() sseEvent {
		t.Helper()
		select {
		case ev := <-events:
			return ev
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for an event")
			return sseEvent{}
		}
	}

	// the initial snapshot, in order
	for _, want := range []string{"history", "points", "kpis", "matches"} {
		if ev := next(); ev.name != want {
			t.Fatalf("got event %q, want %q", ev.name, want)
		}
	}

	// a new sample streams chart JSON and re-rendered HTML
	tel.Sample(time.UnixMilli(5000), matchmaking.QueueStats{Queued: 9}, sim.Stats{})
	chart := next()
	var s Sample
	if chart.name != "chart" || json.Unmarshal([]byte(chart.data), &s) != nil || s.Queued != 9 {
		t.Fatalf("chart event = %+v", chart)
	}
	kpis := next()
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(kpis.data))
	if kpis.name != "kpis" || doc.Find(".tile .value").First().Text() != "9" {
		t.Errorf("kpis event = %+v", kpis)
	}
}

func TestWriteEventPrefixesEveryLine(t *testing.T) {
	var b strings.Builder
	writeEvent(&b, "kpis", []byte("<div>\n<span>1</span>\n</div>"))
	want := "event: kpis\ndata: <div>\ndata: <span>1</span>\ndata: </div>\n\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestFormatting(t *testing.T) {
	nums := []struct {
		v      float64
		digits int
		want   string
	}{
		{1234567.891, 2, "1,234,567.89"},
		{999, 0, "999"},
		{-1500, 0, "−1,500"},
		{-0.4, 0, "0"},
		{0.98765, 3, "0.988"},
	}
	for _, n := range nums {
		if got := formatNum(n.v, n.digits); got != n.want {
			t.Errorf("formatNum(%v, %d) = %q, want %q", n.v, n.digits, got, n.want)
		}
	}

	places := map[int]string{0: "Unplaced", 1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd"}
	for p, want := range places {
		if got := formatPlace(p); got != want {
			t.Errorf("formatPlace(%d) = %q, want %q", p, got, want)
		}
	}

	if formatSigned(17.6) != "+18" || formatSigned(-4) != "−4" || formatSigned(0.2) != "0" {
		t.Errorf("formatSigned: %q %q %q", formatSigned(17.6), formatSigned(-4), formatSigned(0.2))
	}
}
