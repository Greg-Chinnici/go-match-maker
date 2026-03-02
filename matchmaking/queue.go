package matchmaking

import (
	"fmt"
	"go-match-maker/glicko"
	"math"
	"sync"

	"github.com/google/uuid"
)

type Queue struct {
	Mu            sync.Mutex
	Players       []*glicko.Player
	Registry      map[string]*glicko.Player
	ActiveMatches map[string]*ActiveMatch
}
type ActiveMatch struct {
	ID      string
	Player1 *glicko.Player
	Player2 *glicko.Player
}

func NewQueue() *Queue {
	return &Queue{
		Players:       make([]*glicko.Player, 0),
		Registry:      make(map[string]*glicko.Player),
		ActiveMatches: make(map[string]*ActiveMatch),
	}
}
func (q *Queue) AddPlayer(p *glicko.Player) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	if _, ok := q.Registry[p.ID]; ok {
		fmt.Println("Player already exists in Queue")
		return
	}

	q.Players = append(q.Players, p)
	q.Registry[p.ID] = p
}

func (q *Queue) Snapshot() ([]*glicko.Player, int) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	players := make([]*glicko.Player, len(q.Players))
	copy(players, q.Players)

	matchCount := len(q.ActiveMatches)
	return players, matchCount
}

func (q *Queue) ProcessMatches(maxSkillDiff float64) []*Match {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	maxPingDelta := 50.0
	var matches []*Match
	// instead of NxN make it use buckets of maxSkillDiff
	i := 0
	for i < len(q.Players) {
		j := i + 1
		found := false

		for j < len(q.Players) {
			skillDelta := math.Abs(q.Players[i].Rating - q.Players[j].Rating)
			pingDelta := math.Abs(q.Players[i].AvgPing - q.Players[j].AvgPing)

			if skillDelta <= maxSkillDiff && pingDelta <= maxPingDelta {
				p1 := q.Players[i]
				p2 := q.Players[j]

				matchID := uuid.New().String()

				q.ActiveMatches[matchID] = &ActiveMatch{
					ID:      matchID,
					Player1: p1,
					Player2: p2,
				}

				matches = append(matches, &Match{
					Player1: p1,
					Player2: p2,
				})

				// Remove j first (higher index)
				q.Players = removeAt(q.Players, j)
				q.Players = removeAt(q.Players, i)

				found = true
				break
			}
			j++
		}

		if !found {
			i++
		}
	}

	return matches
}

func removeAt(s []*glicko.Player, index int) []*glicko.Player {
	return append(s[:index], s[index+1:]...)
}
