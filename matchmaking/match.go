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
	switch gameType {
	case "FFA":
		return NewFFAConfig(lobbySize)
	case "TDM":
		return NewCasualTeamDeathmatch(lobbySize)
	case "BR":
		return NewBattleRoyale(lobbySize, teamCount)
	default:
		panic("Invalid Match Config choice. (FFA , TDM , BR)")
	}

}
