package matchmaking

import (
	"errors"
	"testing"
	"time"

	"go-match-maker/glicko"
)

func TestConfigFactory(t *testing.T) {
	tests := []struct {
		gameType         string
		lobby, teamCount int
		wantTeams        int
		wantErr          bool
	}{
		{"FFA", 2, 0, 2, false},
		{"FFA", 8, 0, 8, false},
		{"TDM", 10, 0, 2, false},
		{"BR", 24, 6, 6, false},
		{"FFA", 1, 0, 0, true},  // nobody to play against
		{"TDM", 5, 0, 0, true},  // odd lobby can't split into 2 teams
		{"BR", 24, 5, 0, true},  // 24 doesn't split into 5
		{"BR", 4, 6, 0, true},   // more teams than players
		{"BR", 24, 1, 0, true},  // one team is not a match
		{"CTF", 10, 2, 0, true}, // unknown mode
	}
	for _, tt := range tests {
		config, err := ConfigFactory(tt.gameType, tt.lobby, tt.teamCount)
		if (err != nil) != tt.wantErr {
			t.Errorf("ConfigFactory(%s, %d, %d) error = %v, wantErr %v", tt.gameType, tt.lobby, tt.teamCount, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && config.TeamCount != tt.wantTeams {
			t.Errorf("ConfigFactory(%s, %d, %d) TeamCount = %d, want %d", tt.gameType, tt.lobby, tt.teamCount, config.TeamCount, tt.wantTeams)
		}
	}
}

func queueWith(t *testing.T, ps ...*glicko.Player) *Queue {
	t.Helper()
	q := NewQueue()
	for _, p := range ps {
		if err := q.AddPlayer(p); err != nil {
			t.Fatal(err)
		}
	}
	return q
}

func TestAddPlayerRejectsDuplicates(t *testing.T) {
	p := glicko.NewPlayer("dup")
	q := queueWith(t, p)

	if err := q.AddPlayer(glicko.NewPlayer("dup")); !errors.Is(err, ErrAlreadyQueued) {
		t.Errorf("AddPlayer duplicate error = %v, want ErrAlreadyQueued", err)
	}
	if queued, _ := q.Snapshot(); queued != 1 {
		t.Errorf("queue size = %d, want 1", queued)
	}
}

func TestProcessMatchesGroupsPlayersWithinRatingWindow(t *testing.T) {
	// two tight pairs and one player too far from anyone
	q := queueWith(t, players(1500, 1520, 1800, 1830, 2400)...)
	config, err := ConfigFactory("FFA", 2, 0)
	if err != nil {
		t.Fatal(err)
	}

	matches, err := q.ProcessMatches(100, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	for _, m := range matches {
		low, high := m.Teams[0].Players[0].Rating, m.Teams[1].Players[0].Rating
		if high-low >= 100 {
			t.Errorf("match %s pairs %.0f with %.0f, outside the 100 window", m.ID, low, high)
		}
	}

	queued, active := q.Snapshot()
	if queued != 1 || active != 2 {
		t.Errorf("queued=%d active=%d, want 1 and 2", queued, active)
	}
	// matched players stay registered until their match is reported
	if len(q.RegistrySnapshot()) != 5 {
		t.Errorf("registry has %d players, want 5", len(q.RegistrySnapshot()))
	}
}

func TestProcessMatchesSlidesPastOutliers(t *testing.T) {
	// 1000 can't play with anyone; the window should slide and match the rest
	q := queueWith(t, players(1000, 1500, 1510, 1520, 1530)...)
	config, _ := ConfigFactory("TDM", 4, 0)

	matches, err := q.ProcessMatches(100, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1", len(matches))
	}
	for _, team := range matches[0].Teams {
		for _, p := range team.Players {
			if p.Rating == 1000 {
				t.Error("outlier was matched")
			}
		}
	}
}

func singleMatch(t *testing.T) (*Queue, *ActiveMatch) {
	t.Helper()
	q := queueWith(t, players(1500, 1510)...)
	config, _ := ConfigFactory("FFA", 2, 0)
	matches, err := q.ProcessMatches(100, config)
	if err != nil || len(matches) != 1 {
		t.Fatalf("setup: %d matches, err %v", len(matches), err)
	}
	return q, matches[0]
}

func TestClaimMatchOnlyOnce(t *testing.T) {
	q, m := singleMatch(t)

	if _, ok := q.ClaimMatch(m.ID); !ok {
		t.Fatal("first claim failed")
	}
	if _, ok := q.ClaimMatch(m.ID); ok {
		t.Error("second claim succeeded")
	}

	q.RestoreMatch(m)
	if _, ok := q.ClaimMatch(m.ID); !ok {
		t.Error("claim after restore failed")
	}
}

func TestCompleteMatchAppliesRatingsAndReleasesPlayers(t *testing.T) {
	q, m := singleMatch(t)
	claimed, _ := q.ClaimMatch(m.ID)

	winner := claimed.Teams[0].Players[0]
	updated := map[string]glicko.Player{
		winner.ID: {ID: winner.ID, Rating: 1600, RD: 90, Volatility: 0.059},
	}
	q.CompleteMatch(claimed, updated)

	if winner.Rating != 1600 || winner.RD != 90 || winner.Volatility != 0.059 {
		t.Errorf("winner = %+v, want rating 1600 rd 90 vol 0.059", *winner)
	}
	if n := len(q.RegistrySnapshot()); n != 0 {
		t.Errorf("registry has %d players after completion, want 0", n)
	}
	// released players can queue again
	if err := q.AddPlayer(winner); err != nil {
		t.Errorf("re-queue after match: %v", err)
	}
}

func TestActiveMatchesSnapshotIsACopy(t *testing.T) {
	q, m := singleMatch(t)

	matches := q.ActiveMatchesSnapshot()
	matches[m.ID].Teams[0].Players[0].Rating = -1

	if m.Teams[0].Players[0].Rating == -1 {
		t.Error("editing a snapshot changed the queued player")
	}
}

func TestQueueTracksWaitTimes(t *testing.T) {
	q := queueWith(t, players(1500, 1510, 2500)...)
	time.Sleep(20 * time.Millisecond)

	config, _ := ConfigFactory("FFA", 2, 0)
	matches, err := q.ProcessMatches(100, config)
	if err != nil || len(matches) != 1 {
		t.Fatalf("got %d matches, err %v; want 1", len(matches), err)
	}

	m := matches[0]
	if m.CreatedAt.IsZero() {
		t.Error("match has no CreatedAt")
	}
	if m.AvgWait < 20*time.Millisecond || m.MaxWait < m.AvgWait {
		t.Errorf("avg wait %v, max wait %v; want avg >= 20ms and max >= avg", m.AvgWait, m.MaxWait)
	}

	// the 2500 player is still waiting and is now the oldest
	stats := q.Stats()
	if stats.Queued != 1 || stats.Active != 1 {
		t.Errorf("stats = %+v, want 1 queued and 1 active", stats)
	}
	if stats.OldestWait < 20*time.Millisecond {
		t.Errorf("oldest wait %v, want >= 20ms", stats.OldestWait)
	}
}
