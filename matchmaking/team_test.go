package matchmaking

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	"go-match-maker/glicko"
)

// players returns one player per rating, IDs p0, p1, ..., sorted the way the
// queue hands them to a strategy (ascending rating).
func players(ratings ...float64) []*glicko.Player {
	ps := make([]*glicko.Player, len(ratings))
	for i, r := range ratings {
		ps[i] = glicko.EstablishedPlayer(r, 100, glicko.DefaultVolatility, fmt.Sprintf("p%d", i))
	}
	slices.SortStableFunc(ps, func(a, b *glicko.Player) int {
		return cmp.Compare(a.Rating, b.Rating)
	})
	return ps
}

func evenRatings(n int) []float64 {
	r := make([]float64, n)
	for i := range r {
		r[i] = 1400 + float64(i)*10
	}
	return r
}

func teamIDs(teams []Team) [][]string {
	ids := make([][]string, len(teams))
	for i, team := range teams {
		for _, p := range team.Players {
			ids[i] = append(ids[i], p.ID)
		}
	}
	return ids
}

func TestStrategiesAssignEveryPlayerOnce(t *testing.T) {
	strategies := map[string]MatchStrategy{
		"snake":  SnakeDraftTeam{},
		"random": RandomTeam{},
		"greedy": OptimalTeam{},
	}
	shapes := []struct{ lobby, teams int }{{2, 2}, {4, 2}, {6, 3}, {12, 4}, {24, 6}, {10, 2}}

	for name, strategy := range strategies {
		for _, shape := range shapes {
			t.Run(fmt.Sprintf("%s/%dx%d", name, shape.lobby, shape.teams), func(t *testing.T) {
				lobby := players(evenRatings(shape.lobby)...)
				teams, err := strategy.BuildMatch(lobby, shape.teams)
				if err != nil {
					t.Fatal(err)
				}
				if len(teams) != shape.teams {
					t.Fatalf("got %d teams, want %d", len(teams), shape.teams)
				}

				seen := map[string]bool{}
				for i, team := range teams {
					if len(team.Players) != shape.lobby/shape.teams {
						t.Errorf("team %d has %d players, want %d", i, len(team.Players), shape.lobby/shape.teams)
					}
					if team.ID == "" {
						t.Errorf("team %d has no ID", i)
					}
					for _, p := range team.Players {
						if seen[p.ID] {
							t.Errorf("player %s assigned twice", p.ID)
						}
						seen[p.ID] = true
					}
				}
				if len(seen) != shape.lobby {
					t.Errorf("%d players assigned, want %d", len(seen), shape.lobby)
				}
			})
		}
	}
}

func TestStrategiesRejectBadTeamCounts(t *testing.T) {
	strategies := map[string]MatchStrategy{
		"snake":  SnakeDraftTeam{},
		"random": RandomTeam{},
		"greedy": OptimalTeam{},
	}
	for name, strategy := range strategies {
		t.Run(name, func(t *testing.T) {
			if _, err := strategy.BuildMatch(players(1500, 1500), 0); err == nil {
				t.Error("zero teams: expected an error")
			}
			if _, err := strategy.BuildMatch(players(1500, 1500), 3); err == nil {
				t.Error("more teams than players: expected an error")
			}
		})
	}
}

func TestSnakeDraftOrder(t *testing.T) {
	tests := []struct {
		name    string
		ratings []float64
		teams   int
		want    [][]string
	}{
		{
			// p3 picks first, then p2 and p1 both go to the other team
			name:    "4 players 2 teams",
			ratings: []float64{1000, 1100, 1200, 1300},
			teams:   2,
			want:    [][]string{{"p2", "p1"}, {"p3", "p0"}},
		},
		{
			name:    "6 players 3 teams",
			ratings: []float64{1000, 1100, 1200, 1300, 1400, 1500},
			teams:   3,
			want:    [][]string{{"p3", "p2"}, {"p4", "p1"}, {"p5", "p0"}},
		},
		{
			name:    "single team",
			ratings: []float64{1000, 1100, 1200},
			teams:   1,
			want:    [][]string{{"p2", "p1", "p0"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teams, err := SnakeDraftTeam{}.BuildMatch(players(tt.ratings...), tt.teams)
			if err != nil {
				t.Fatal(err)
			}
			if got := teamIDs(teams); !slices.EqualFunc(got, tt.want, slices.Equal) {
				t.Errorf("teams = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSnakeDraftBalancesEvenSpread(t *testing.T) {
	teams, err := SnakeDraftTeam{}.BuildMatch(players(evenRatings(8)...), 2)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := teams[0].AverageRating(), teams[1].AverageRating(); a != b {
		t.Errorf("team averages %.1f and %.1f, want equal for an even spread", a, b)
	}
}

func TestGreedyDraftCapsTeamSize(t *testing.T) {
	// Without a cap the 3500 player's team would get nobody else and the
	// other team all three 1500s.
	teams, err := OptimalTeam{}.BuildMatch(players(1500, 1500, 1500, 3500), 2)
	if err != nil {
		t.Fatal(err)
	}
	for i, team := range teams {
		if len(team.Players) != 2 {
			t.Errorf("team %d has %d players, want 2", i, len(team.Players))
		}
	}
}

func TestGreedyDraftBalancesTotals(t *testing.T) {
	teams, err := OptimalTeam{}.BuildMatch(players(1000, 1200, 1300, 1500), 2)
	if err != nil {
		t.Fatal(err)
	}
	// 1500+1000 vs 1300+1200
	if a, b := teams[0].AverageRating(), teams[1].AverageRating(); a != b {
		t.Errorf("team averages %.1f and %.1f, want equal", a, b)
	}
}

func TestFFAOnePlayerPerTeam(t *testing.T) {
	lobby := players(1500, 1510, 1520)
	teams, err := FFATeam{}.BuildMatch(lobby, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != len(lobby) {
		t.Fatalf("got %d teams, want %d", len(teams), len(lobby))
	}
	for i, team := range teams {
		if len(team.Players) != 1 || team.Players[0] != lobby[i] {
			t.Errorf("team %d = %v, want just %s", i, teamIDs([]Team{team})[0], lobby[i].ID)
		}
	}

	if _, err := (FFATeam{}).BuildMatch(nil, 0); err == nil {
		t.Error("no players: expected an error")
	}
}

func TestFirstPlayerByUUID(t *testing.T) {
	team := Team{Players: []*glicko.Player{
		glicko.NewPlayer("c"), glicko.NewPlayer("a"), glicko.NewPlayer("b"),
	}}
	if got := team.FirstPlayerByUUID(); got != "a" {
		t.Errorf("FirstPlayerByUUID() = %q, want %q", got, "a")
	}
	if got := (Team{}).FirstPlayerByUUID(); got != "" {
		t.Errorf("empty team FirstPlayerByUUID() = %q, want empty", got)
	}
}
