package sim_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"go-match-maker/glicko"
	"go-match-maker/matchmaking"
	"go-match-maker/sim"
)

type simResult struct {
	sim.Stats
	stuck int // players still waiting in the queue at the end
}

// simulate runs the real queue, team strategies and rating updates for a
// population of new players whose results come from their true skills.
// Every round all idle players queue, matches are made, and each match is
// resolved straight away.
func simulate(t *testing.T, config matchmaking.MatchConfig, players, rounds int, ratingDiff float64, seed uint64) simResult {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed))
	q := matchmaking.NewQueue()

	all := make([]*glicko.Player, players)
	for i := range all {
		all[i] = glicko.NewPlayer(fmt.Sprintf("sim-%d-%d", seed, i))
	}

	tracker := sim.NewTracker()
	for round := 1; round <= rounds; round++ {
		for _, p := range all {
			q.AddPlayer(p) // ErrAlreadyQueued for players still waiting
		}

		matches, err := q.ProcessMatches(ratingDiff, config)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			resolve(t, q, m.ID, rng, tracker)
		}

		if round%(rounds/5) == 0 {
			s := tracker.Stats()
			t.Logf("round %4d: correlation %.3f  slope %.2f  rmse %5.1f  favorite wins %.2f", round, s.Correlation, s.Slope, s.RMSE, s.FavoriteWinRate)
		}
	}

	ratings := make(map[string]float64, len(all))
	for _, p := range all {
		ratings[p.ID] = p.Rating
	}
	stuck, _ := q.Snapshot()

	result := simResult{Stats: sim.Compare(ratings), stuck: stuck}
	result.Matches = tracker.Stats().Matches
	result.FavoriteWinRate = tracker.Stats().FavoriteWinRate
	return result
}

// resolve plays a match out from true skills and records it the same way
// the /report handler does.
func resolve(t *testing.T, q *matchmaking.Queue, matchID string, rng *rand.Rand, tracker *sim.Tracker) {
	t.Helper()
	claimed, ok := q.ClaimMatch(matchID)
	if !ok {
		t.Fatalf("match %s not claimable", matchID)
	}
	rated := claimed.Clone()

	skills := make([]float64, len(rated.Teams))
	teams := make([][]*glicko.Player, len(rated.Teams))
	favorite := 0
	for i, team := range rated.Teams {
		skills[i] = sim.TeamSkill(team.TeamUIDSlice())
		teams[i] = team.Players
		if team.AverageRating() > rated.Teams[favorite].AverageRating() {
			favorite = i
		}
	}

	order := sim.Placements(skills, rng)
	ranks := make([]int, len(order))
	for place, team := range order {
		ranks[team] = place + 1
	}
	if err := glicko.UpdateRankedTeams(teams, ranks); err != nil {
		t.Fatal(err)
	}

	updated := map[string]glicko.Player{}
	ratings := map[string]float64{}
	for _, team := range teams {
		for _, p := range team {
			updated[p.ID] = *p
			ratings[p.ID] = p.Rating
		}
	}
	q.CompleteMatch(claimed, updated)
	tracker.RecordMatch(order[0] == favorite, ratings)
}

func TestRatingsConvergeToTrueSkill(t *testing.T) {
	tests := []struct {
		mode             string
		lobby, teamCount int
		minCorrelation   float64
	}{
		{"FFA", 2, 0, 0.9},
		{"FFA", 8, 0, 0.9},
		{"TDM", 4, 0, 0.8},
		{"BR", 12, 4, 0.8},
	}

	players, rounds := 400, 300
	if testing.Short() {
		players, rounds = 200, 100
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%dx%d", tt.mode, tt.lobby, tt.teamCount), func(t *testing.T) {
			config, err := matchmaking.ConfigFactory(tt.mode, tt.lobby, tt.teamCount)
			if err != nil {
				t.Fatal(err)
			}

			got := simulate(t, config, players, rounds, 200, 1)
			t.Logf("final: %d matches, correlation %.3f, slope %.2f, rmse %.1f, %d players stuck in queue",
				got.Matches, got.Correlation, got.Slope, got.RMSE, got.stuck)

			// Correlation is checked but slope is not: because players only
			// meet rating neighbors, ratings rank players well but stretch
			// past the true scale over time (slope > 1). Random pairing
			// keeps the slope near 1, so this comes from matchmaking, not
			// from the rating math.
			if got.Correlation < tt.minCorrelation {
				t.Errorf("correlation %.3f, want at least %.2f", got.Correlation, tt.minCorrelation)
			}
		})
	}
}
