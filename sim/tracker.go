package sim

import (
	"math"
	"sync"
)

// Stats compares current ratings with true skills.
type Stats struct {
	Players int `json:"players"`
	Matches int `json:"matches"`

	// FavoriteWinRate is how often the team with the highest pre-match
	// rating won. It rises as ratings start predicting results.
	FavoriteWinRate float64 `json:"favorite_win_rate"`

	// Correlation is the Pearson correlation between rating and true
	// skill, and RMSE the root-mean-square gap between them.
	Correlation float64 `json:"correlation"`
	RMSE        float64 `json:"rmse"`

	// Slope is the regression slope of rating on true skill: 1 means
	// ratings are on the right scale, above 1 means they are stretched.
	Slope float64 `json:"slope"`
}

// Tracker records each player's latest rating from reported matches. It is
// safe for concurrent use.
type Tracker struct {
	mu           sync.Mutex
	ratings      map[string]float64
	order        []string // player IDs in first-seen order, for stable samples
	matches      int
	favoriteWins int
}

func NewTracker() *Tracker {
	return &Tracker{ratings: make(map[string]float64)}
}

// Point is one player's current rating next to their true skill.
type Point struct {
	Rating float64 `json:"r"`
	Skill  float64 `json:"s"`
}

// Points returns up to limit players, always the earliest-seen ones, so a
// chart redrawn from successive calls shows the same players moving.
func (t *Tracker) Points(limit int) []Point {
	t.mu.Lock()
	defer t.mu.Unlock()

	n := min(limit, len(t.order))
	points := make([]Point, n)
	for i, id := range t.order[:n] {
		points[i] = Point{Rating: t.ratings[id], Skill: TrueSkill(id)}
	}
	return points
}

// RecordMatch stores the post-match ratings (keyed by player ID) and whether
// the pre-match favorite won.
func (t *Tracker) RecordMatch(favoriteWon bool, ratings map[string]float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.matches++
	if favoriteWon {
		t.favoriteWins++
	}
	for id, r := range ratings {
		if _, ok := t.ratings[id]; !ok {
			t.order = append(t.order, id)
		}
		t.ratings[id] = r
	}
}

func (t *Tracker) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()

	stats := Compare(t.ratings)
	stats.Matches = t.matches
	if t.matches > 0 {
		stats.FavoriteWinRate = float64(t.favoriteWins) / float64(t.matches)
	}
	return stats
}

// Compare measures how well ratings (keyed by player ID) match true skills.
func Compare(ratings map[string]float64) Stats {
	n := float64(len(ratings))
	stats := Stats{Players: len(ratings)}
	if n < 2 {
		return stats
	}

	var sumR, sumS, sumSqErr float64
	for id, r := range ratings {
		s := TrueSkill(id)
		sumR += r
		sumS += s
		sumSqErr += (r - s) * (r - s)
	}
	meanR, meanS := sumR/n, sumS/n

	var cov, varR, varS float64
	for id, r := range ratings {
		dr, ds := r-meanR, TrueSkill(id)-meanS
		cov += dr * ds
		varR += dr * dr
		varS += ds * ds
	}

	if varR > 0 && varS > 0 {
		stats.Correlation = cov / math.Sqrt(varR*varS)
	}
	if varS > 0 {
		stats.Slope = cov / varS
	}
	stats.RMSE = math.Sqrt(sumSqErr / n)
	return stats
}

// Histogram counts ratings and true skills in shared bins of width binWidth,
// so the two distributions can be drawn on one axis.
type Histogram struct {
	BinStart []float64 `json:"bins"` // lower edge of each bin
	Ratings  []int     `json:"ratings"`
	Skills   []int     `json:"skills"`
}

func NewHistogram(points []Point, binWidth float64) Histogram {
	if len(points) == 0 {
		return Histogram{}
	}

	lo, hi := points[0].Rating, points[0].Rating
	for _, p := range points {
		lo = min(lo, p.Rating, p.Skill)
		hi = max(hi, p.Rating, p.Skill)
	}
	lo = math.Floor(lo/binWidth) * binWidth
	bins := int((hi-lo)/binWidth) + 1

	h := Histogram{
		BinStart: make([]float64, bins),
		Ratings:  make([]int, bins),
		Skills:   make([]int, bins),
	}
	for i := range bins {
		h.BinStart[i] = lo + float64(i)*binWidth
	}
	for _, p := range points {
		h.Ratings[int((p.Rating-lo)/binWidth)]++
		h.Skills[int((p.Skill-lo)/binWidth)]++
	}
	return h
}
