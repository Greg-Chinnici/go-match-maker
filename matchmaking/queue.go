package matchmaking

import (
	"errors"
	"go-match-maker/glicko"
	"sync"
	"time"

	"github.com/google/btree"
	"github.com/google/uuid"
)

type Queue struct {
	Mu            sync.Mutex
	Registry      map[string]*glicko.Player
	ActiveMatches map[string]*ActiveMatch

	PlayersQueued *btree.BTree

	queuedAt map[string]time.Time // when each waiting player joined
}
type ActiveMatch struct {
	ID    string
	Teams []Team

	CreatedAt time.Time
	AvgWait   time.Duration // mean time its players spent in the queue
	MaxWait   time.Duration
}

// Clone copies the match down to its players, so the copy can be read or
// re-rated without touching the queue's players.
func (m *ActiveMatch) Clone() *ActiveMatch {
	c := *m
	c.Teams = make([]Team, len(m.Teams))
	for i, team := range m.Teams {
		c.Teams[i] = Team{ID: team.ID, Players: make([]*glicko.Player, len(team.Players))}
		for j, p := range team.Players {
			player := *p
			c.Teams[i].Players[j] = &player
		}
	}
	return &c
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

		queuedAt: make(map[string]time.Time),
	}
}

// ErrAlreadyQueued means the player is already waiting or in an active match.
var ErrAlreadyQueued = errors.New("player is already queued or in a match")

func (q *Queue) AddPlayer(p *glicko.Player) error {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	if _, ok := q.Registry[p.ID]; ok {
		return ErrAlreadyQueued
	}

	q.PlayersQueued.ReplaceOrInsert(PlayerItem{Player: p})
	q.Registry[p.ID] = p
	q.queuedAt[p.ID] = time.Now()
	return nil
}

type QueueStats struct {
	Queued     int
	Active     int
	OldestWait time.Duration // how long the longest-waiting player has waited
}

func (q *Queue) Stats() QueueStats {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	stats := QueueStats{Queued: q.PlayersQueued.Len(), Active: len(q.ActiveMatches)}
	now := time.Now()
	for _, at := range q.queuedAt {
		stats.OldestWait = max(stats.OldestWait, now.Sub(at))
	}
	return stats
}

// RegistrySnapshot copies every queued or in-match player, so callers can
// read them without holding the lock.
func (q *Queue) RegistrySnapshot() map[string]glicko.Player {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	players := make(map[string]glicko.Player, len(q.Registry))
	for id, p := range q.Registry {
		players[id] = *p
	}
	return players
}

// ActiveMatchesSnapshot deep copies every active match, so callers can read
// them without holding the lock.
func (q *Queue) ActiveMatchesSnapshot() map[string]*ActiveMatch {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	matches := make(map[string]*ActiveMatch, len(q.ActiveMatches))
	for id, m := range q.ActiveMatches {
		matches[id] = m.Clone()
	}
	return matches
}

// ClaimMatch removes a match from the active set so only one report can
// resolve it. The caller must follow up with CompleteMatch, or RestoreMatch
// if the result could not be recorded.
func (q *Queue) ClaimMatch(id string) (*ActiveMatch, bool) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	m, ok := q.ActiveMatches[id]
	if ok {
		delete(q.ActiveMatches, id)
	}
	return m, ok
}

// RestoreMatch puts a claimed match back so its result can be reported again.
func (q *Queue) RestoreMatch(m *ActiveMatch) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	q.ActiveMatches[m.ID] = m
}

// CompleteMatch applies the post-match ratings in updated (keyed by player ID)
// to a claimed match's players and releases them so they can queue again.
func (q *Queue) CompleteMatch(m *ActiveMatch, updated map[string]glicko.Player) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	for _, team := range m.Teams {
		for _, p := range team.Players {
			if u, ok := updated[p.ID]; ok {
				p.Rating, p.RD, p.Volatility = u.Rating, u.RD, u.Volatility
			}
			delete(q.Registry, p.ID)
		}
	}
}

func (q *Queue) Snapshot() (int, int) {
	q.Mu.Lock()
	defer q.Mu.Unlock()

	players := q.PlayersQueued.Len()
	matchCount := len(q.ActiveMatches)
	return players, matchCount
}

// ProcessMatches forms matches from queued players and returns copies of
// them, so callers can read the players without holding the lock.
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

				q.recordWaits(m, window)
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
		delete(q.queuedAt, p.Player.ID)
	}
	snapshots := make([]*ActiveMatch, len(newMatches))
	for i, m := range newMatches {
		q.ActiveMatches[m.ID] = m
		snapshots[i] = m.Clone()
	}
	return snapshots, nil
}

// recordWaits stamps a new match with how long its players queued.
func (q *Queue) recordWaits(m *ActiveMatch, players []*glicko.Player) {
	m.CreatedAt = time.Now()

	var total time.Duration
	for _, p := range players {
		wait := m.CreatedAt.Sub(q.queuedAt[p.ID])
		total += wait
		m.MaxWait = max(m.MaxWait, wait)
	}
	m.AvgWait = total / time.Duration(len(players))
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
