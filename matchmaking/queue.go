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
	Teams []Team
}

type PlayerItem struct {
	Player *glicko.Player
}

func (p PlayerItem) Less(than btree.Item) bool {
	a := p.Player
	b := than.(PlayerItem).Player

	if a.Rating != b.Rating {
		return a.Rating < b.Rating
	}

	return a.ID < b.ID
}

func NewQueue() *Queue {
	return &Queue{
		Registry:      make(map[string]*glicko.Player),
		ActiveMatches: make(map[string]*ActiveMatch),

		PlayersQueued: btree.New(16),
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

func (q *Queue) ProcessMatches(maxSkillDiff float64, config MatchConfig) ([]*ActiveMatch, error) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	window := []*glicko.Player{}
	var toRemove []PlayerItem
	var newMatches []*ActiveMatch

	var processErr error

	q.PlayersQueued.Ascend(func(item btree.Item) bool {
		p := item.(PlayerItem).Player

		window = append(window, p)

		if len(window) == config.LobbySize {

			if isValidMatch(window, maxSkillDiff) {
				m, err := createMatch(window, config.Strategy, config.TeamCount)
				if err != nil {
					processErr = err
					return false
				}

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

	if processErr != nil {
		return nil, processErr
	}

	for _, p := range toRemove {
		q.PlayersQueued.Delete(p)
	}
	for _, m := range newMatches {
		q.ActiveMatches[m.ID] = m
	}
	return newMatches, nil
}

func isValidMatch(lobby []*glicko.Player, maxRatingDiff float64) bool {
	return lobby[len(lobby)-1].Rating-lobby[0].Rating < maxRatingDiff
}

// make this create match more generic
func createMatch(lobby []*glicko.Player, strategy MatchStrategy, teamCount int) (*ActiveMatch, error) {
	teams, err := strategy.BuildMatch(lobby, teamCount)
	if err != nil {
		return nil, err
	}

	return &ActiveMatch{Teams: teams, ID: uuid.NewString()}, nil
}

func removeAt(s []*glicko.Player, index int) []*glicko.Player {
	return append(s[:index], s[index+1:]...)
}
