package server

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

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

	// Placements lists team IDs in finishing order, best first. Teams left
	// out finished behind every listed team, in unknown order. When empty,
	// Winner alone is treated as the only placement.
	Placements []string `json:"placements,omitempty"`
}
type ReportResponse struct {
	TeamRatings   map[string]float64 `json:"team_ratings"`
	PlayerRatings map[string]float64 `json:"player_ratings"`
}
type QueueRequest struct {
	UID string `json:"uid"`
}

func RegisterHandlers(queue *matchmaking.Queue) {
	http.Handle("/", newMux(queue))
}

func newMux(queue *matchmaking.Queue) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /queue", func(w http.ResponseWriter, r *http.Request) {
		var req QueueRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		var p *glicko.Player

		if req.UID != "" {
			p, err = store.FetchPlayer(r.Context(), req.UID)
			if err != nil {
				http.Error(w, "db error", http.StatusInternalServerError)
				return
			}
		}

		// If not found → create new
		if p == nil {
			p = glicko.NewPlayer(req.UID)

			if err := store.SavePlayers(r.Context(), []*glicko.Player{p}); err != nil {
				http.Error(w, "failed to create player", http.StatusInternalServerError)
				return
			}
		}

		if err := queue.AddPlayer(p); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		writeJSON(w, map[string]any{
			"player_id": p.ID,
		})
	})

	mux.HandleFunc("GET /ratings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, queue.RegistrySnapshot())
	})

	activeMatches := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, queue.ActiveMatchesSnapshot())
	}
	mux.HandleFunc("GET /active-matches", activeMatches)
	mux.HandleFunc("GET /matches", activeMatches)

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		players, totalMatches := queue.Snapshot()

		writeJSON(w, Status{
			QueueSize:     players,
			ActiveMatches: totalMatches,
		})
	})

	mux.HandleFunc("GET /sim/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, SimStats.Stats())
	})

	mux.HandleFunc("POST /report", func(w http.ResponseWriter, r *http.Request) {
		var req ReportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		// Claiming takes the match out of the active set, so a duplicate
		// report gets a 404 while this one is still being saved, and the
		// queue lock is not held during the database write.
		match, ok := queue.ClaimMatch(req.MatchID)
		if !ok {
			http.Error(w, "match not found", http.StatusNotFound)
			return
		}

		ranks, err := placementRanks(match, req)
		if err != nil {
			queue.RestoreMatch(match)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// rate copies so nothing changes in memory until the result is saved
		rated := match.Clone()
		teams := make([][]*glicko.Player, len(rated.Teams))
		before := make([]float64, len(rated.Teams))
		var players []*glicko.Player
		for i, team := range rated.Teams {
			teams[i] = team.Players
			before[i] = team.AverageRating()
			players = append(players, team.Players...)
		}
		if err := glicko.UpdateRankedTeams(teams, ranks); err != nil {
			queue.RestoreMatch(match)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := store.SavePlayers(r.Context(), players); err != nil {
			queue.RestoreMatch(match)
			http.Error(w, "failed to save ratings", http.StatusInternalServerError)
			return
		}
		fmt.Printf("Reported match %s, placements %v\n", match.ID, ranks)

		updated := make(map[string]glicko.Player, len(players))
		resp := ReportResponse{
			TeamRatings:   make(map[string]float64, len(rated.Teams)),
			PlayerRatings: make(map[string]float64, len(players)),
		}
		for _, team := range rated.Teams {
			resp.TeamRatings[team.ID] = team.AverageRating()
		}
		for _, p := range players {
			updated[p.ID] = *p
			resp.PlayerRatings[p.ID] = p.Rating
		}
		queue.CompleteMatch(match, updated)
		telemetry.RecordReported(matchEvent(rated, ranks, before))

		writeJSON(w, resp)
	})

	registerDashboard(mux)
	mux.Handle("GET /static/", http.FileServerFS(staticFS))

	return mux
}

// placementRanks turns a report into one glicko rank per team in match.Teams.
func placementRanks(match *matchmaking.ActiveMatch, req ReportRequest) ([]int, error) {
	placements := req.Placements
	if len(placements) == 0 {
		if req.Winner == "" {
			return nil, fmt.Errorf("report needs winner_id or placements")
		}
		placements = []string{req.Winner}
	}

	teamIndex := make(map[string]int, len(match.Teams))
	for i, team := range match.Teams {
		teamIndex[team.ID] = i
	}

	ranks := make([]int, len(match.Teams)) // zero value is glicko.Unplaced
	for place, id := range placements {
		i, ok := teamIndex[id]
		if !ok {
			return nil, fmt.Errorf("team %s not found in match", id)
		}
		if ranks[i] != glicko.Unplaced {
			return nil, fmt.Errorf("team %s placed more than once", id)
		}
		ranks[i] = place + 1
	}

	return ranks, nil
}

// matchEvent summarizes a rated match for the dashboard feed, best placement
// first and unplaced teams last.
func matchEvent(rated *matchmaking.ActiveMatch, ranks []int, before []float64) MatchEvent {
	ev := MatchEvent{ID: rated.ID, Time: time.Now()}
	for i, team := range rated.Teams {
		ev.Teams = append(ev.Teams, TeamOutcome{
			Place:   ranks[i],
			Players: len(team.Players),
			Before:  before[i],
			After:   team.AverageRating(),
		})
	}

	slices.SortStableFunc(ev.Teams, func(a, b TeamOutcome) int {
		if (a.Place == glicko.Unplaced) != (b.Place == glicko.Unplaced) {
			return cmp.Compare(btoi(a.Place == glicko.Unplaced), btoi(b.Place == glicko.Unplaced))
		}
		return cmp.Compare(a.Place, b.Place)
	})
	return ev
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
