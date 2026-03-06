package matchmaking

import (
	"go-match-maker/glicko"
)

type MatchConfig struct {
	LobbySize int
	TeamCount int
	Strategy  MatchStrategy
}
type MatchStrategy interface {
	BuildMatch(players []*glicko.Player, teamCount int) ([]Team, error)
}

func ConfigFactory(gameType string, lobbySize, teamCount int) MatchConfig {
	var strategy MatchStrategy
	switch gameType {
	case "FFA":
		strategy = FFATeam{}
	case "TDM":
		strategy = RandomTeam{}
	default:
		panic("Invalid Match Config choice")
	}

	return MatchConfig{lobbySize, teamCount, strategy}
}
