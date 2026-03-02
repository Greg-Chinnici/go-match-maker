package matchmaking

import (
	"go-match-maker/glicko"
	"sort"
)

type Team struct {
	Players []*glicko.Player
}

type ActiveMatchTeam struct {
	ID    string
	TeamA Team
	TeamB Team
}

func (t Team) AverageRating() float64 {
	sum := 0.0
	for _, p := range t.Players {
		sum += p.Rating
	}
	return sum / float64(len(t.Players))
}

func BalanceTeamsGreedy(players []*glicko.Player) (Team, Team) {
	sort.Slice(players, func(i, j int) bool {
		return players[i].Rating > players[j].Rating // sort descending
	})

	teamA := Team{}
	teamB := Team{}

	for _, p := range players {
		if teamA.AverageRating() <= teamB.AverageRating() && len(teamA.Players) < len(players)/2 {
			teamA.Players = append(teamA.Players, p)
		} else if len(teamB.Players) < len(players)/2 {
			teamB.Players = append(teamB.Players, p)
		} else {
			// fill the rest of the spots if one team is already full
			teamA.Players = append(teamA.Players, p)
		}
	}
	return teamA, teamB
}
