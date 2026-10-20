package glicko

import (
	"math"
	"testing"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.5f, want %.5f (±%g)", name, got, want, tol)
	}
}

func player(rating, rd float64) *Player {
	return EstablishedPlayer(rating, rd, DefaultVolatility, "")
}

// Worked example from Glickman's "Example of the Glicko-2 system".
func TestUpdatePaperExample(t *testing.T) {
	p := player(1500, 200)
	p.UpdateWithTau(
		[]*Player{player(1400, 30), player(1550, 100), player(1700, 300)},
		[]float64{1, 0, 0},
		0.5,
	)

	near(t, "rating", p.Rating, 1464.06, 0.01)
	near(t, "rd", p.RD, 151.52, 0.01)
	near(t, "volatility", p.Volatility, 0.05999, 0.00001)
}

func TestUpdateWithNoGamesOnlyGrowsRD(t *testing.T) {
	p := player(1700, 100)
	p.Update(nil, nil)

	near(t, "rating", p.Rating, 1700, 0)
	near(t, "rd", p.RD, 173.7178*math.Sqrt(math.Pow(100/173.7178, 2)+DefaultVolatility*DefaultVolatility), 1e-9)
}

func TestExpectedScore(t *testing.T) {
	a, b := player(1500, 100), player(1500, 100)
	near(t, "equal players", a.ExpectedScore(b), 0.5, 1e-12)

	strong, weak := player(1800, 100), player(1500, 100)
	near(t, "sum of both sides", strong.ExpectedScore(weak)+weak.ExpectedScore(strong), 1, 1e-12)
	if strong.ExpectedScore(weak) <= 0.5 {
		t.Errorf("stronger player expected score %.3f, want > 0.5", strong.ExpectedScore(weak))
	}
}

func TestUpdateMatchIsSymmetric(t *testing.T) {
	a, b := player(1500, 200), player(1500, 200)
	UpdateMatch(a, b, a)

	near(t, "gain equals loss", a.Rating-1500, 1500-b.Rating, 1e-9)
	near(t, "same rd", a.RD, b.RD, 1e-9)
	if a.Rating <= 1500 {
		t.Errorf("winner rating %.2f, want > 1500", a.Rating)
	}
}

func TestUpdateMatchDrawBetweenEqualsKeepsRating(t *testing.T) {
	a, b := player(1500, 200), player(1500, 200)
	UpdateMatch(a, b, nil)

	near(t, "a rating", a.Rating, 1500, 1e-9)
	near(t, "b rating", b.Rating, 1500, 1e-9)
}

// Team B must be rated against team A's pre-match ratings, not the ratings
// team A has just been given.
func TestUpdateTeamMatchUsesPreMatchRatings(t *testing.T) {
	teamA := []*Player{player(1500, 200), player(1500, 200)}
	teamB := []*Player{player(1500, 200), player(1500, 200)}
	UpdateTeamMatch(teamA, teamB, 1)

	near(t, "gain equals loss", teamA[0].Rating-1500, 1500-teamB[0].Rating, 1e-9)
	near(t, "same rd", teamA[0].RD, teamB[0].RD, 1e-9)
}

func TestUpdateTeamMatchOrderDoesNotMatter(t *testing.T) {
	newTeams := func() ([]*Player, []*Player) {
		return []*Player{player(1600, 80), player(1450, 250)},
			[]*Player{player(1550, 120), player(1500, 300)}
	}

	a1, b1 := newTeams()
	UpdateTeamMatch(a1, b1, 1)

	a2, b2 := newTeams()
	UpdateTeamMatch(b2, a2, -1)

	for i := range a1 {
		near(t, "team A rating", a1[i].Rating, a2[i].Rating, 1e-9)
		near(t, "team B rating", b1[i].Rating, b2[i].Rating, 1e-9)
	}
}

func TestUpdateRankedTeamsWinnerOnly(t *testing.T) {
	teams := [][]*Player{
		{player(1500, 200), player(1500, 200)},
		{player(1500, 200), player(1500, 200)},
		{player(1500, 200), player(1500, 200)},
		{player(1500, 200), player(1500, 200)},
	}
	if err := UpdateRankedTeams(teams, []int{1, Unplaced, Unplaced, Unplaced}); err != nil {
		t.Fatal(err)
	}

	// The winner beat three teams but got one rating update, so it should
	// gain more than a single win and far less than three separate updates.
	single := []*Player{player(1500, 200)}
	UpdateTeamMatch(single, []*Player{player(1500, 200)}, 1)
	winnerGain := teams[0][0].Rating - 1500
	if winnerGain <= single[0].Rating-1500 {
		t.Errorf("winner gained %.2f, want more than one win's %.2f", winnerGain, single[0].Rating-1500)
	}

	// Losers only lost to the winner, so they all move the same amount.
	for i := 1; i < len(teams); i++ {
		near(t, "loser rating", teams[i][0].Rating, teams[1][0].Rating, 1e-9)
		near(t, "single loss", teams[i][0].Rating, 1500-(single[0].Rating-1500), 1e-9)
	}
}

func TestUpdateRankedTeamsFullPlacements(t *testing.T) {
	teams := make([][]*Player, 4)
	for i := range teams {
		teams[i] = []*Player{player(1500, 200)}
	}
	if err := UpdateRankedTeams(teams, []int{3, 1, 4, 2}); err != nil {
		t.Fatal(err)
	}

	byPlace := []float64{teams[1][0].Rating, teams[3][0].Rating, teams[0][0].Rating, teams[2][0].Rating}
	for i := 1; i < len(byPlace); i++ {
		if byPlace[i] >= byPlace[i-1] {
			t.Errorf("place %d rating %.2f not below place %d rating %.2f", i+1, byPlace[i], i, byPlace[i-1])
		}
	}
	near(t, "1st and 4th mirror", byPlace[0]-1500, 1500-byPlace[3], 1e-9)
	near(t, "2nd and 3rd mirror", byPlace[1]-1500, 1500-byPlace[2], 1e-9)
}

func TestUpdateRankedTeamsTiesAreDraws(t *testing.T) {
	teams := [][]*Player{{player(1500, 200)}, {player(1500, 200)}}
	if err := UpdateRankedTeams(teams, []int{1, 1}); err != nil {
		t.Fatal(err)
	}

	near(t, "rating", teams[0][0].Rating, 1500, 1e-9)
	if teams[0][0].RD >= 200 {
		t.Errorf("rd %.2f, want a draw to shrink it below 200", teams[0][0].RD)
	}
}

func TestUpdateRankedTeamsAllUnplacedLeavesRatingsAlone(t *testing.T) {
	teams := [][]*Player{{player(1600, 200)}, {player(1400, 200)}}
	if err := UpdateRankedTeams(teams, []int{Unplaced, Unplaced}); err != nil {
		t.Fatal(err)
	}

	near(t, "rating", teams[0][0].Rating, 1600, 0)
	near(t, "rd", teams[0][0].RD, 200, 0)
}

func TestUpdateRankedTeamsRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		teams [][]*Player
		ranks []int
	}{
		{"rank count mismatch", [][]*Player{{player(1500, 200)}, {player(1500, 200)}}, []int{1}},
		{"empty team", [][]*Player{{player(1500, 200)}, {}}, []int{1, 2}},
		{"negative rank", [][]*Player{{player(1500, 200)}, {player(1500, 200)}}, []int{1, -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UpdateRankedTeams(tt.teams, tt.ranks); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
