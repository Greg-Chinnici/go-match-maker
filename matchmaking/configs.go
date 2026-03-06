package matchmaking

func NewFFAConfig(lobbySize int) MatchConfig {
	return MatchConfig{
		LobbySize: lobbySize,
		TeamCount: lobbySize,
		Strategy:  FFATeam{},
	}
}

func NewCasualTeamDeathmatch(lobbySize int) MatchConfig {
	return MatchConfig{
		LobbySize: lobbySize,
		TeamCount: 2,
		Strategy:  SnakeDraftTeam{},
	}
}

func NewBattleRoyale(lobbySize, teamCount int) MatchConfig {

	return MatchConfig{
		LobbySize: lobbySize,
		TeamCount: teamCount,
		Strategy:  RandomTeam{},
	}
}
