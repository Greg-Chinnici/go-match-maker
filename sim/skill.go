// Package sim simulates match results from hidden "true" skills, so tests can
// check that ratings converge toward something real.
package sim

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"strconv"

	"github.com/google/uuid"
)

const (
	SkillMean   = 1500.0
	SkillSpread = 300.0
)

// TrueSkill returns a player's hidden skill on the Glicko rating scale.
// Skills are normally distributed (SkillMean, SkillSpread) and derived from a
// hash of the ID, so every process agrees on them without sharing state.
func TrueSkill(id string) float64 {
	sum := sha256.Sum256([]byte(id))
	// top 53 bits as a uniform in (0, 1), then the inverse normal CDF
	u := (float64(binary.BigEndian.Uint64(sum[:8])>>11) + 0.5) / (1 << 53)
	return SkillMean + SkillSpread*math.Sqrt2*math.Erfinv(2*u-1)
}

// playerNamespace keeps simulated player IDs apart from any other UUIDv5s.
var playerNamespace = uuid.MustParse("6f0c4a8e-2b1d-4c3f-9a5e-7d8b9c0e1f2a")

// PlayerID is the UUID of simulated player n. The same n always gives the
// same ID, so seeding and load runs share one population of players.
func PlayerID(n int) string {
	return uuid.NewSHA1(playerNamespace, []byte(strconv.Itoa(n))).String()
}

// TeamSkill is the mean true skill of a team's players.
func TeamSkill(ids []string) float64 {
	sum := 0.0
	for _, id := range ids {
		sum += TrueSkill(id)
	}
	return sum / float64(len(ids))
}

// WinProbability is the chance that skill a beats skill b, the same logistic
// curve Glicko-2 assumes between two players with no rating uncertainty.
func WinProbability(a, b float64) float64 {
	return 1 / (1 + math.Pow(10, (b-a)/400))
}

// Placements samples a finishing order for teams with the given skills and
// returns team indices, best first. It uses a Plackett-Luce model weighting
// each team by 10^(skill/400), so any two teams finish in a given order with
// probability WinProbability. A nil rng uses the global source.
func Placements(skills []float64, rng *rand.Rand) []int {
	if len(skills) == 0 {
		return nil
	}
	float := rand.Float64
	if rng != nil {
		float = rng.Float64
	}

	best := skills[0]
	for _, s := range skills {
		best = max(best, s)
	}

	remaining := make([]int, len(skills))
	weights := make([]float64, len(skills))
	for i, s := range skills {
		remaining[i] = i
		weights[i] = math.Pow(10, (s-best)/400) // shifted so the largest is 1
	}

	order := make([]int, 0, len(skills))
	for len(remaining) > 0 {
		total := 0.0
		for _, i := range remaining {
			total += weights[i]
		}

		pick := len(remaining) - 1 // fallback for rounding at the top end
		r := float() * total
		for k, i := range remaining {
			if r < weights[i] {
				pick = k
				break
			}
			r -= weights[i]
		}

		order = append(order, remaining[pick])
		remaining = append(remaining[:pick], remaining[pick+1:]...)
	}

	return order
}
