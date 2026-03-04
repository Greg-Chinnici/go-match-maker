package matchmaking

import (
	"fmt"
	"go-match-maker/glicko"
	"math/rand"

	"github.com/google/uuid"
)

type Team struct {
	Players []*glicko.Player
	ID      string
}

func (t Team) AverageRating() float64 {
	sum := 0.0
	for _, p := range t.Players {
		sum += p.Rating
	}
	return sum / float64(len(t.Players))
}

func (t Team) TeamUIDSlice() []string {
	uids := make([]string, 0, len(t.Players))

	for _, p := range t.Players {
		uids = append(uids, p.ID)
	}

	return uids
}

type ITeamBalance interface {
	TeamBalance(players []*glicko.Player, teamCount int) ([]Team, error)
}

type SnakeDraftTeam struct{}

func (s SnakeDraftTeam) TeamBalance(players []*glicko.Player, teamCount int) ([]Team, error) {
	if teamCount <= 0 {
		return nil, fmt.Errorf("invalid team count")
	}
	if len(players) < teamCount {
		return nil, fmt.Errorf("not enough players")
	}

	teams := make([]Team, teamCount)
	for i := range teams {
		teams[i] = Team{ID: uuid.NewString()}
	}

	index := teamCount - 1
	direction := -1

	for i := len(players) - 1; i >= 0; i-- {
		teams[index].Players = append(teams[index].Players, players[i])

		index += direction

		if index < 0 {
			index = 1
			direction = 1
		}
		if index >= teamCount {
			index = teamCount - 2
			direction = -1
		}
	}

	return teams, nil
}

type RandomTeam struct{}

func (r RandomTeam) TeamBalance(players []*glicko.Player, teamCount int) ([]Team, error) {
	if teamCount <= 0 {
		return nil, fmt.Errorf("invalid team count")
	}
	if len(players) < teamCount {
		return nil, fmt.Errorf("not enough players")
	}

	rand.Shuffle(len(players), func(i, j int) {
		players[i], players[j] = players[j], players[i]
	})

	teams := make([]Team, teamCount)
	for i := range teams {
		teams[i] = Team{ID: uuid.NewString()}
	}

	for i, p := range players {
		teamIndex := i % teamCount
		teams[teamIndex].Players = append(teams[teamIndex].Players, p)
	}

	return teams, nil
}

type FFATeam struct{}

func (f FFATeam) BuildMatch(players []*glicko.Player, teamCount int) ([]Team, error) {
	if len(players) == 0 {
		return nil, fmt.Errorf("no players")
	}

	teams := make([]Team, len(players))

	for i, p := range players {
		teams[i] = Team{
			ID:      uuid.NewString(),
			Players: []*glicko.Player{p},
		}
	}

	return teams, nil
}

type OptimalTeam struct{}
