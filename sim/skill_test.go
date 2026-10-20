package sim

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestTrueSkillIsDeterministicAndNormal(t *testing.T) {
	if TrueSkill("player-1") != TrueSkill("player-1") {
		t.Fatal("same ID gave different skills")
	}

	const n = 20000
	var sum, sumSq float64
	for i := range n {
		s := TrueSkill(fmt.Sprintf("player-%d", i))
		sum += s
		sumSq += s * s
	}
	mean := sum / n
	sd := math.Sqrt(sumSq/n - mean*mean)

	if math.Abs(mean-SkillMean) > 10 {
		t.Errorf("mean skill %.1f, want about %.0f", mean, SkillMean)
	}
	if math.Abs(sd-SkillSpread) > 10 {
		t.Errorf("skill spread %.1f, want about %.0f", sd, SkillSpread)
	}
}

func TestPlacementsIsAPermutation(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 1))
	for n := 1; n <= 8; n++ {
		skills := make([]float64, n)
		for i := range skills {
			skills[i] = 1200 + rng.Float64()*600
		}

		order := Placements(skills, rng)
		sorted := slices.Sorted(slices.Values(order))
		for i, v := range sorted {
			if v != i {
				t.Fatalf("Placements(%d teams) = %v, not a permutation", n, order)
			}
		}
	}

	if Placements(nil, rng) != nil {
		t.Error("no teams should give no placements")
	}
}

func TestPlacementsMatchesWinProbability(t *testing.T) {
	rng := rand.New(rand.NewPCG(2, 2))
	skills := []float64{1500, 1700, 1300}
	const trials = 40000

	// count how often team i finishes ahead of team j
	ahead := [3][3]int{}
	for range trials {
		order := Placements(skills, rng)
		for a := range order {
			for b := a + 1; b < len(order); b++ {
				ahead[order[a]][order[b]]++
			}
		}
	}

	for i := range skills {
		for j := range skills {
			if i == j {
				continue
			}
			got := float64(ahead[i][j]) / trials
			want := WinProbability(skills[i], skills[j])
			if math.Abs(got-want) > 0.01 {
				t.Errorf("team %d ahead of team %d %.3f of the time, want %.3f", i, j, got, want)
			}
		}
	}
}

func TestWinProbability(t *testing.T) {
	if p := WinProbability(1500, 1500); p != 0.5 {
		t.Errorf("equal skills: %.3f, want 0.5", p)
	}
	// 400 points is 10:1 odds
	if p := WinProbability(1900, 1500); math.Abs(p-10.0/11) > 1e-12 {
		t.Errorf("400 point edge: %.4f, want %.4f", p, 10.0/11)
	}
}

func TestCompare(t *testing.T) {
	perfect := map[string]float64{}
	shifted := map[string]float64{}
	for i := range 500 {
		id := fmt.Sprintf("p%d", i)
		perfect[id] = TrueSkill(id)
		shifted[id] = TrueSkill(id) + 100
	}

	if s := Compare(perfect); math.Abs(s.Correlation-1) > 1e-9 || s.RMSE > 1e-9 {
		t.Errorf("perfect ratings: %+v, want correlation 1 and RMSE 0", s)
	}
	if s := Compare(perfect); math.Abs(s.Slope-1) > 1e-9 {
		t.Errorf("perfect ratings slope %.3f, want 1", s.Slope)
	}
	if s := Compare(shifted); math.Abs(s.Correlation-1) > 1e-9 || math.Abs(s.RMSE-100) > 1e-9 {
		t.Errorf("shifted ratings: %+v, want correlation 1 and RMSE 100", s)
	}

	stretched := map[string]float64{}
	for id, r := range perfect {
		stretched[id] = SkillMean + 1.5*(r-SkillMean)
	}
	if s := Compare(stretched); math.Abs(s.Slope-1.5) > 1e-9 {
		t.Errorf("stretched ratings slope %.3f, want 1.5", s.Slope)
	}

	flat := map[string]float64{"a": 1500, "b": 1500, "c": 1500}
	if s := Compare(flat); s.Correlation != 0 {
		t.Errorf("flat ratings correlation %.3f, want 0", s.Correlation)
	}
}

func TestTracker(t *testing.T) {
	tr := NewTracker()
	tr.RecordMatch(true, map[string]float64{"a": 1600, "b": 1400})
	tr.RecordMatch(false, map[string]float64{"a": 1550, "c": 1500})

	s := tr.Stats()
	if s.Matches != 2 || s.Players != 3 || s.FavoriteWinRate != 0.5 {
		t.Errorf("stats = %+v, want 2 matches, 3 players, 0.5 favorite win rate", s)
	}
}

func TestPlayerID(t *testing.T) {
	if PlayerID(7) != PlayerID(7) {
		t.Fatal("same n gave different IDs")
	}
	seen := map[string]bool{}
	for n := range 1000 {
		id := PlayerID(n)
		if _, err := uuid.Parse(id); err != nil {
			t.Fatalf("PlayerID(%d) = %q is not a UUID: %v", n, id, err)
		}
		if seen[id] {
			t.Fatalf("PlayerID(%d) repeats an earlier ID", n)
		}
		seen[id] = true
	}
}

func TestTrackerPointsAreStable(t *testing.T) {
	tr := NewTracker()
	tr.RecordMatch(true, map[string]float64{"a": 1600})
	tr.RecordMatch(true, map[string]float64{"b": 1400, "c": 1500})
	tr.RecordMatch(true, map[string]float64{"a": 1650}) // update, not a new player

	points := tr.Points(2)
	if len(points) != 2 {
		t.Fatalf("got %d points, want 2", len(points))
	}
	if points[0] != (Point{Rating: 1650, Skill: TrueSkill("a")}) {
		t.Errorf("first point = %+v, want a's latest rating", points[0])
	}
	if got := tr.Points(10); len(got) != 3 {
		t.Errorf("limit above player count gave %d points, want 3", len(got))
	}
}

func TestHistogram(t *testing.T) {
	points := []Point{
		{Rating: 1410, Skill: 1590},
		{Rating: 1450, Skill: 1610},
		{Rating: 1720, Skill: 1499},
	}
	h := NewHistogram(points, 100)

	wantBins := []float64{1400, 1500, 1600, 1700}
	if !slices.Equal(h.BinStart, wantBins) {
		t.Fatalf("bins = %v, want %v", h.BinStart, wantBins)
	}
	if !slices.Equal(h.Ratings, []int{2, 0, 0, 1}) {
		t.Errorf("rating counts = %v", h.Ratings)
	}
	if !slices.Equal(h.Skills, []int{1, 1, 1, 0}) {
		t.Errorf("skill counts = %v", h.Skills)
	}

	if empty := NewHistogram(nil, 100); len(empty.BinStart) != 0 {
		t.Errorf("empty histogram has %d bins", len(empty.BinStart))
	}
}
