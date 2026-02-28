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

func RegisterHandlers(queue *matchmaking.Queue) {

	http.HandleFunc("/queue", func(w http.ResponseWriter, r *http.Request) {

		p := glicko.NewPlayer()
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

	http.HandleFunc("/matches", func(w http.ResponseWriter, r *http.Request) {
		queue.Mu.Lock()
		defer queue.Mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(queue.ActiveMatches)
	})

	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		players, matches := queue.Snapshot()

		resp := Status{
			QueueSize:     len(players),
			ActiveMatches: matches,
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

		p1 := match.Player1
		p2 := match.Player2

		fmt.Println(p1.ExpectedScore(p2))
		fmt.Println(p2.ExpectedScore(p1))

		if req.Winner == p1.ID {
			glicko.UpdateMatch(p1, p2, p1)
		} else {
			glicko.UpdateMatch(p1, p2, p2)
		}
		fmt.Printf("Winner %s\n", req.Winner)

		delete(queue.ActiveMatches, req.MatchID)

		json.NewEncoder(w).Encode(map[string]any{
			"p1_rating": p1.Rating,
			"p2_rating": p2.Rating,
		})
	})
}
