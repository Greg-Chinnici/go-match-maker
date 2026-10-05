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
	var config MatchConfig
	switch gameType {
	case "FFA":
		config = NewFFAConfig(lobbySize)
	case "TDM":
		config = NewCasualTeamDeathmatch(lobbySize)
	case "BR":
		config = NewBattleRoyale(lobbySize, teamCount)
	default:
		return NewFFAConfig(1), fmt.Errorf("invalid match config: %s", gameType)
	}

	if err := config.Validate(); err != nil {
		return NewFFAConfig(1), fmt.Errorf("invalid %s config: %w", gameType, err)
	}
	return config, nil
}

// Validate checks that every lobby can be split into equal teams.
func (c MatchConfig) Validate() error {
	if c.TeamCount < 2 {
		return fmt.Errorf("need at least 2 teams, got %d", c.TeamCount)
	}
	if c.LobbySize < c.TeamCount {
		return fmt.Errorf("lobby size %d is smaller than team count %d", c.LobbySize, c.TeamCount)
	}
	if c.LobbySize%c.TeamCount != 0 {
		return fmt.Errorf("lobby size %d does not split evenly into %d teams", c.LobbySize, c.TeamCount)
	}
	return nil
}
