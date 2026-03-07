package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"go-match-maker/glicko"
	"go-match-maker/matchmaking"
)

type Status struct {
	QueueSize     int `json:"queue_size"`
	ActiveMatches int `json:"active_matches"`
}
type ReportRequest struct {
	MatchID string `json:"match_id"`
	Winner  string `json:"winner_id"`
}
type QueueRequest struct {
	UID string `json:"uid"`
}

func RegisterHandlers(queue *matchmaking.Queue) {

	http.HandleFunc("/queue", func(w http.ResponseWriter, r *http.Request) {
		var req QueueRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil && err.Error() != "EOF" {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		var p *glicko.Player

		if req.UID != "" {
			p, err = TryFetchPlayer(req.UID)
			if err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}

			// If not found → create new
			if p == nil {
				p = glicko.NewPlayer(req.UID)

				if err := SavePlayer(p); err != nil {
					http.Error(w, "failed to create player", http.StatusInternalServerError)
					return
				}
			}
		} else {
			p = glicko.NewPlayer("")
			if err := SavePlayer(p); err != nil {
				http.Error(w, "failed to create player", http.StatusInternalServerError)
				return
			}
		}

		queue.AddPlayer(p)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"player_id": p.ID,
		})
	})

	http.HandleFunc("/ratings", func(w http.ResponseWriter, r *http.Request) {
		queue.Mu.Lock()
		defer queue.Mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(queue.Registry)
	})

	http.HandleFunc("/active-matches", func(w http.ResponseWriter, r *http.Request) {
		queue.Mu.Lock()
		defer queue.Mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(queue.ActiveMatches)
	})

	http.HandleFunc("/matches", func(w http.ResponseWriter, r *http.Request) {
		queue.Mu.Lock()
		defer queue.Mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(queue.ActiveMatches)
	})

	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		players, totalMatches := queue.Snapshot()

		resp := Status{
			QueueSize:     players,
			ActiveMatches: totalMatches,
		}

		json.NewEncoder(w).Encode(resp)
	})

	http.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {

		var req ReportRequest
		json.NewDecoder(r.Body).Decode(&req)

		queue.Mu.Lock()
		defer queue.Mu.Unlock()

		match, ok := queue.ActiveMatches[req.MatchID]
		if !ok {
			http.Error(w, "match not found", http.StatusNotFound)
			return
		}

		var winnerTeam *matchmaking.Team
		var loserTeams []matchmaking.Team

		for i := range match.Teams {
			if match.Teams[i].ID == req.Winner {
				winnerTeam = &match.Teams[i]
			} else {
				loserTeams = append(loserTeams, match.Teams[i])
			}
		}

		if winnerTeam == nil {
			http.Error(w, "winner not found in match", http.StatusBadRequest)
			return
		}
		fmt.Printf("Winner %s\n", req.Winner)

		for _, loser := range loserTeams {
			glicko.UpdateTeamMatch(winnerTeam.Players, loser.Players, 1)
		}
		for _, team := range match.Teams {
			SavePlayersInTx(team.Players)
		}

		for _, team := range match.Teams {
			for _, p := range team.Players {
				defer delete(queue.Registry, p.ID)
			}
		}

		defer delete(queue.ActiveMatches, req.MatchID)

		ratings := make(map[string]float64)

		for _, team := range match.Teams {
			ratings[team.ID] = team.AverageRating()
		}

		json.NewEncoder(w).Encode(map[string]any{
			"team_ratings": ratings,
		})

	})
}
