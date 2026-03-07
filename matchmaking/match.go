package matchmaking

import (
	"fmt"
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

func ConfigFactory(gameType string, lobbySize, teamCount int) (MatchConfig, error) {
	switch gameType {
	case "FFA":
		return NewFFAConfig(lobbySize), nil
	case "TDM":
		return NewCasualTeamDeathmatch(lobbySize), nil
	case "BR":
		return NewBattleRoyale(lobbySize, teamCount), nil
	default:
		return NewFFAConfig(1), fmt.Errorf("invalid match config: %s", gameType)
	}

}
