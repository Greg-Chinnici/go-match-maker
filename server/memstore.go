package server

import (
	"context"
	"sync"

	"go-match-maker/glicko"
)

// MemoryStore keeps players in process memory. Ratings are lost on exit, so
// it is for demos and load tests without a database.
type MemoryStore struct {
	mu      sync.Mutex
	players map[string]glicko.Player
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{players: make(map[string]glicko.Player)}
}

func (m *MemoryStore) FetchPlayer(_ context.Context, id string) (*glicko.Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.players[id]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (m *MemoryStore) SavePlayers(_ context.Context, players []*glicko.Player) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range players {
		m.players[p.ID] = *p
	}
	return nil
}

// UseStore replaces the store handlers read and write players through.
// Postgres is the default.
func UseStore(s PlayerStore) {
	store = s
}
