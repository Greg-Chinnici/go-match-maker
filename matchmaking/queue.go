package matchmaking

import (
	"fmt"
	"go-match-maker/glicko"
	"sync"

	"github.com/google/btree"
	"github.com/google/uuid"
)

type Queue struct {
	Mu            sync.Mutex
	Registry      map[string]*glicko.Player
	ActiveMatches map[string]*ActiveMatch

	PlayersQueued *btree.BTree
}
type ActiveMatch struct {
	ID    string
	Team1 Team
	Team2 Team
}

type PlayerItem struct {
	Player *glicko.Player
}

func (p PlayerItem) Less(than btree.Item) bool {
	other := than.(PlayerItem).Player

	if p.Player.Rating == other.Rating {
		return p.Player.ID < other.ID
	}
	return p.Player.Rating < other.Rating
}

func NewQueue() *Queue {
	return &Queue{
		Registry:      make(map[string]*glicko.Player),
		ActiveMatches: make(map[string]*ActiveMatch),

		PlayersQueued: btree.New(3),
	}
}
func (q *Queue) AddPlayer(p *glicko.Player) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	if _, ok := q.Registry[p.ID]; ok {
		fmt.Println("Player already exists in Queue")
		return
	}

	q.PlayersQueued.ReplaceOrInsert(PlayerItem{Player: p})
	q.Registry[p.ID] = p
}

func (q *Queue) Snapshot() (int, int) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	players := q.PlayersQueued.Len()
	matchCount := len(q.ActiveMatches)
	return players, matchCount
}

func (q *Queue) ProcessMatches(maxSkillDiff float64, teamSize int) []*ActiveMatch {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	window := []*glicko.Player{}
	var toRemove []PlayerItem
	var newMatches []*ActiveMatch

	q.PlayersQueued.Ascend(func(item btree.Item) bool {
		p := item.(PlayerItem).Player

		window = append(window, p)

		if len(window) == 2*teamSize {

			if isValidMatch(window, maxSkillDiff) {
				m := createMatch(window)
				newMatches = append(newMatches, m)

				for _, p := range window {
					toRemove = append(toRemove, PlayerItem{Player: p})
				}

				window = window[:0]
			} else {
				// slide window by 1
				window = window[1:]
			}
		}

		return true
	})

	for _, p := range toRemove {
		q.PlayersQueued.Delete(p)
	}
	for _, m := range newMatches {
		q.ActiveMatches[m.ID] = m
	}
	return newMatches
}

func isValidMatch(lobby []*glicko.Player, maxRatingDiff float64) bool {
	return lobby[len(lobby)-1].Rating-lobby[0].Rating < maxRatingDiff
}
func createMatch(lobby []*glicko.Player) *ActiveMatch {
	team1, team2 := BalanceTeamsGreedy(lobby)
	return &ActiveMatch{Team1: team1, Team2: team2, ID: uuid.NewString()}
}

func removeAt(s []*glicko.Player, index int) []*glicko.Player {
	return append(s[:index], s[index+1:]...)
}
