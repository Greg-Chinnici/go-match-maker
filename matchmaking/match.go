package matchmaking

import (
	"go-match-maker/glicko"
	"math"
)

type Match struct {
	Player1 *glicko.Player
	Player2 *glicko.Player
}

func FindMatch(queue []*glicko.Player, maxDiff float64) *Match {
	for i := 0; i < len(queue); i++ {
		for j := i + 1; j < len(queue); j++ {
			if math.Abs(queue[i].Rating-queue[j].Rating) <= maxDiff {
				return &Match{
					Player1: queue[i],
					Player2: queue[j],
				}
			}
		}
	}
	return nil
}
