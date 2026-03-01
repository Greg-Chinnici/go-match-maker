package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"go-match-maker/glicko"
	"go-match-maker/matchmaking"

	"github.com/jackc/pgx/v5/pgconn"
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
			if p == nil || err != nil {
				http.Error(w, "player not found", http.StatusNotFound)
				return
			}

			fmt.Println("Player found from DB")
		} else {
			p = glicko.NewPlayer()
		}

		err = SavePlayer(p)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				fmt.Printf("Postgres error code: %s", pgErr.Code)
				fmt.Printf("Message: %s", pgErr.Message)
				fmt.Printf("Detail: %s", pgErr.Detail)
			} else {
				fmt.Printf("Non-PG error: %+v", err)
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
			QueueSize:     len(players),
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

		p1 := match.Player1
		p2 := match.Player2

		//fmt.Println(p1.ExpectedScore(p2))
		//fmt.Println(p2.ExpectedScore(p1))

		if req.Winner == p1.ID {
			glicko.UpdateMatch(p1, p2, p1)
		} else {
			glicko.UpdateMatch(p1, p2, p2)
		}
		fmt.Printf("Winner %s\n", req.Winner)

		delete(queue.ActiveMatches, req.MatchID)

		if err := SavePlayer(p1); err != nil {
			http.Error(w, "failed to save player1", http.StatusInternalServerError)
			return
		}

		if err := SavePlayer(p2); err != nil {
			http.Error(w, "failed to save player2", http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"p1_rating": p1.Rating,
			"p2_rating": p2.Rating,
		})

	})
}
