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

func (q *Queue) ProcessMatches(maxDiff float64) (*Match, bool) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	fmt.Println("Attempting to Match Players")
	maxPingDelta := 50.

	// Change to Skill and Region Buckets isntead of this nxn matching

	for i := 0; i < len(q.Players); i++ {
		for j := i + 1; j < len(q.Players); j++ {

			skillDelta := math.Abs(q.Players[i].Rating - q.Players[j].Rating)
			pingDelta := math.Abs(q.Players[i].AvgPing - q.Players[j].AvgPing)

			if math.Abs(skillDelta) <= maxDiff {
				if math.Abs(pingDelta) > maxPingDelta {
					continue
				}

				p1 := q.Players[i]
				p2 := q.Players[j]

				matchID := uuid.New().String()

				q.ActiveMatches[matchID] = &ActiveMatch{
					ID:      matchID,
					Player1: p1,
					Player2: p2,
				}
				q.Players = removeAt(q.Players, j)
				q.Players = removeAt(q.Players, i)

				return &Match{Player1: p1, Player2: p2}, true
			}
		}
	}
	return nil, false
}
func removeAt(s []*glicko.Player, index int) []*glicko.Player {
	return append(s[:index], s[index+1:]...)
}
