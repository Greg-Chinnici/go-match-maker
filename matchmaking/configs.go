package matchmaking




func NewFFAConfig(lobbySize int) MatchConfig {
    return MatchConfig{
        LobbySize: lobbySize,
        TeamCount: lobbySize, 
        Strategy:  FFAStrategy{},
    }
}

func NewCasualTeamDeathmatch(lobbySize int) MatchConfig {
return MatchConfig{
	LobbySize: lobbySize,
	TeamCount: 2,
	Strategy: SnakeDraftTeam{},
}
}
