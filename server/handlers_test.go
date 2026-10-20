package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go-match-maker/glicko"
	"go-match-maker/matchmaking"
	"go-match-maker/sim"
)

type fakeStore struct {
	mu      sync.Mutex
	players map[string]glicko.Player
	saveErr error
}

func (f *fakeStore) FetchPlayer(_ context.Context, id string) (*glicko.Player, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p, ok := f.players[id]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (f *fakeStore) SavePlayers(_ context.Context, players []*glicko.Player) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.saveErr != nil {
		return f.saveErr
	}
	for _, p := range players {
		f.players[p.ID] = *p
	}
	return nil
}

// useFakeStore swaps the package store for the length of a test.
func useFakeStore(t *testing.T, seed ...*glicko.Player) *fakeStore {
	t.Helper()
	fake := &fakeStore{players: map[string]glicko.Player{}}
	for _, p := range seed {
		fake.players[p.ID] = *p
	}

	old := store
	store = fake
	t.Cleanup(func() { store = old })
	return fake
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	return rec
}

func TestQueueCreatesAndQueuesPlayers(t *testing.T) {
	existing := glicko.EstablishedPlayer(1820, 60, 0.05, "known")
	fake := useFakeStore(t, existing)
	q := matchmaking.NewQueue()
	mux := newMux(q)

	if rec := do(t, mux, "POST", "/queue", QueueRequest{UID: "known"}); rec.Code != http.StatusOK {
		t.Fatalf("known player: status %d: %s", rec.Code, rec.Body)
	}
	if got := q.RegistrySnapshot()["known"].Rating; got != 1820 {
		t.Errorf("known player queued with rating %.0f, want stored 1820", got)
	}

	if rec := do(t, mux, "POST", "/queue", QueueRequest{UID: "brand-new"}); rec.Code != http.StatusOK {
		t.Fatalf("new player: status %d: %s", rec.Code, rec.Body)
	}
	if _, ok := fake.players["brand-new"]; !ok {
		t.Error("new player was not saved")
	}

	// an empty body creates a player with a generated ID
	rec := do(t, mux, "POST", "/queue", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty body: status %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["player_id"] == "" {
		t.Error("empty body: no player_id returned")
	}

	if queued, _ := q.Snapshot(); queued != 3 {
		t.Errorf("queue size %d, want 3", queued)
	}
}

func TestQueueRejectsBadRequests(t *testing.T) {
	useFakeStore(t)
	q := matchmaking.NewQueue()
	mux := newMux(q)

	do(t, mux, "POST", "/queue", QueueRequest{UID: "twice"})
	if rec := do(t, mux, "POST", "/queue", QueueRequest{UID: "twice"}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate queue: status %d, want 409", rec.Code)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/queue", bytes.NewBufferString("{not json")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad json: status %d, want 400", rec.Code)
	}

	if rec := do(t, mux, "GET", "/queue", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /queue: status %d, want 405", rec.Code)
	}
}

// matchOf queues players and returns the single match they form.
func matchOf(t *testing.T, q *matchmaking.Queue, mux http.Handler, mode string, lobby, teams int, ids ...string) *matchmaking.ActiveMatch {
	t.Helper()
	for _, id := range ids {
		if rec := do(t, mux, "POST", "/queue", QueueRequest{UID: id}); rec.Code != http.StatusOK {
			t.Fatalf("queue %s: status %d: %s", id, rec.Code, rec.Body)
		}
	}
	config, err := matchmaking.ConfigFactory(mode, lobby, teams)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := q.ProcessMatches(100, config)
	if err != nil || len(matches) != 1 {
		t.Fatalf("got %d matches, err %v; want 1", len(matches), err)
	}
	return matches[0]
}

func TestReportUpdatesRatingsAndReleasesPlayers(t *testing.T) {
	fake := useFakeStore(t)
	q := matchmaking.NewQueue()
	mux := newMux(q)
	m := matchOf(t, q, mux, "TDM", 4, 0, "a", "b", "c", "d")
	winner, loser := m.Teams[0], m.Teams[1]

	rec := do(t, mux, "POST", "/report", ReportRequest{MatchID: m.ID, Winner: winner.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	var resp ReportResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.TeamRatings[winner.ID] <= resp.TeamRatings[loser.ID] {
		t.Errorf("team ratings %v: winner should be above loser", resp.TeamRatings)
	}
	if len(resp.PlayerRatings) != 4 {
		t.Errorf("got %d player ratings, want 4", len(resp.PlayerRatings))
	}
	for _, p := range winner.Players {
		if fake.players[p.ID].Rating <= glicko.DefaultRating {
			t.Errorf("winner %s saved with rating %.1f, want above default", p.ID, fake.players[p.ID].Rating)
		}
		if fake.players[p.ID].Rating != resp.PlayerRatings[p.ID] {
			t.Errorf("saved rating %.1f != reported %.1f", fake.players[p.ID].Rating, resp.PlayerRatings[p.ID])
		}
	}

	if _, active := q.Snapshot(); active != 0 {
		t.Errorf("%d active matches after report, want 0", active)
	}
	if n := len(q.RegistrySnapshot()); n != 0 {
		t.Errorf("%d players still registered, want 0", n)
	}
	if rec := do(t, mux, "POST", "/report", ReportRequest{MatchID: m.ID, Winner: winner.ID}); rec.Code != http.StatusNotFound {
		t.Errorf("second report: status %d, want 404", rec.Code)
	}
}

func TestReportWithPlacements(t *testing.T) {
	fake := useFakeStore(t)
	q := matchmaking.NewQueue()
	mux := newMux(q)
	m := matchOf(t, q, mux, "FFA", 4, 0, "a", "b", "c", "d")

	order := []string{m.Teams[2].ID, m.Teams[0].ID, m.Teams[3].ID, m.Teams[1].ID}
	if rec := do(t, mux, "POST", "/report", ReportRequest{MatchID: m.ID, Placements: order}); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	idOf := func(teamID string) string {
		for _, team := range m.Teams {
			if team.ID == teamID {
				return team.Players[0].ID
			}
		}
		t.Fatalf("no team %s", teamID)
		return ""
	}
	for i := 1; i < len(order); i++ {
		above, below := fake.players[idOf(order[i-1])].Rating, fake.players[idOf(order[i])].Rating
		if below >= above {
			t.Errorf("place %d rated %.1f, not below place %d at %.1f", i+1, below, i, above)
		}
	}
}

func TestReportFailuresKeepMatchReportable(t *testing.T) {
	tests := []struct {
		name    string
		req     func(m *matchmaking.ActiveMatch) ReportRequest
		saveErr error
		want    int
	}{
		{
			name: "unknown winner",
			req:  func(m *matchmaking.ActiveMatch) ReportRequest { return ReportRequest{MatchID: m.ID, Winner: "nope"} },
			want: http.StatusBadRequest,
		},
		{
			name: "no result",
			req:  func(m *matchmaking.ActiveMatch) ReportRequest { return ReportRequest{MatchID: m.ID} },
			want: http.StatusBadRequest,
		},
		{
			name: "duplicate placement",
			req: func(m *matchmaking.ActiveMatch) ReportRequest {
				return ReportRequest{MatchID: m.ID, Placements: []string{m.Teams[0].ID, m.Teams[0].ID}}
			},
			want: http.StatusBadRequest,
		},
		{
			name: "database down",
			req: func(m *matchmaking.ActiveMatch) ReportRequest {
				return ReportRequest{MatchID: m.ID, Winner: m.Teams[0].ID}
			},
			saveErr: errors.New("connection refused"),
			want:    http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useFakeStore(t)
			q := matchmaking.NewQueue()
			mux := newMux(q)
			m := matchOf(t, q, mux, "FFA", 2, 0, "a", "b")
			fake.saveErr = tt.saveErr

			if rec := do(t, mux, "POST", "/report", tt.req(m)); rec.Code != tt.want {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}

			restored, ok := q.ActiveMatchesSnapshot()[m.ID]
			if !ok {
				t.Fatal("match was not restored")
			}
			for _, team := range restored.Teams {
				if r := team.Players[0].Rating; r != glicko.DefaultRating {
					t.Errorf("rating changed to %.1f after a failed report", r)
				}
			}

			// fixing the problem lets the same match be reported
			fake.saveErr = nil
			if rec := do(t, mux, "POST", "/report", ReportRequest{MatchID: m.ID, Winner: m.Teams[0].ID}); rec.Code != http.StatusOK {
				t.Errorf("retry: status %d: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestReadEndpoints(t *testing.T) {
	useFakeStore(t)
	q := matchmaking.NewQueue()
	mux := newMux(q)
	m := matchOf(t, q, mux, "FFA", 2, 0, "a", "b")
	do(t, mux, "POST", "/queue", QueueRequest{UID: "waiting"})

	var status Status
	json.NewDecoder(do(t, mux, "GET", "/status", nil).Body).Decode(&status)
	if status != (Status{QueueSize: 1, ActiveMatches: 1}) {
		t.Errorf("status = %+v, want 1 queued and 1 active", status)
	}

	for _, path := range []string{"/active-matches", "/matches"} {
		var matches map[string]matchmaking.ActiveMatch
		json.NewDecoder(do(t, mux, "GET", path, nil).Body).Decode(&matches)
		if _, ok := matches[m.ID]; !ok || len(matches) != 1 {
			t.Errorf("%s = %v, want just match %s", path, matches, m.ID)
		}
	}

	var ratings map[string]glicko.Player
	json.NewDecoder(do(t, mux, "GET", "/ratings", nil).Body).Decode(&ratings)
	if len(ratings) != 3 {
		t.Errorf("/ratings has %d players, want 3", len(ratings))
	}
}

// HandleMatch should play a match out, report it over HTTP, and record the
// result in SimStats.
func TestHandleMatchReportsPlacements(t *testing.T) {
	fake := useFakeStore(t)
	q := matchmaking.NewQueue()
	mux := newMux(q)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldURL, oldStats := REPORT_API_URL, SimStats
	REPORT_API_URL, SimStats = srv.URL+"/report", sim.NewTracker()
	t.Cleanup(func() { REPORT_API_URL, SimStats = oldURL, oldStats })

	m := matchOf(t, q, mux, "BR", 6, 3, "a", "b", "c", "d", "e", "f")
	HandleMatch(m)

	if _, active := q.Snapshot(); active != 0 {
		t.Fatalf("%d active matches after HandleMatch, want 0", active)
	}
	if len(fake.players) != 6 {
		t.Errorf("%d players saved, want 6", len(fake.players))
	}

	var stats sim.Stats
	json.NewDecoder(do(t, mux, "GET", "/sim/stats", nil).Body).Decode(&stats)
	if stats.Matches != 1 || stats.Players != 6 {
		t.Errorf("sim stats = %+v, want 1 match and 6 players", stats)
	}

	// with full placements every player learns something, even the middle
	// team, whose rating can stay put after one win and one loss
	for id, p := range fake.players {
		if p.RD >= glicko.DefaultRD {
			t.Errorf("player %s RD %.1f, want below the default", id, p.RD)
		}
	}
}
