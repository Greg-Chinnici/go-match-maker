package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"go-match-maker/matchmaking"
	"go-match-maker/sim"
)

// REPORT_API_URL is a var so tests can point the resolver at a test server.
var REPORT_API_URL = "http://localhost:8080/report"

var client = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     30 * time.Second,
	},
}

func StartWorkerPool(workerCount int, jobs <-chan *matchmaking.ActiveMatch) {

	for i := range workerCount {
		go func(id int) {
			for match := range jobs {
				HandleMatch(match)
			}
		}(i)
	}
}

// SimStats tracks how close ratings from resolved matches are to the players'
// hidden true skills. It is served at GET /sim/stats.
var SimStats = sim.NewTracker()

func HandleMatch(match *matchmaking.ActiveMatch) {
	// Real world case would be to send this to the actual match servers
	// then tell the match how to report a finished game
	//
	// as of now it waits a bit, plays the match out from each player's hidden
	// true skill (see the sim package), and reports the finishing order to
	// the current server
	time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond)

	skills := make([]float64, len(match.Teams))
	favorite := 0
	for i, team := range match.Teams {
		skills[i] = sim.TeamSkill(team.TeamUIDSlice())
		if team.AverageRating() > match.Teams[favorite].AverageRating() {
			favorite = i
		}
	}

	order := sim.Placements(skills, nil)
	payload := ReportRequest{
		MatchID:    match.ID,
		Winner:     match.Teams[order[0]].ID,
		Placements: make([]string, len(order)),
	}
	for place, team := range order {
		payload.Placements[place] = match.Teams[team].ID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("json error:", err)
		return
	}

	req, err := http.NewRequest("POST", REPORT_API_URL, bytes.NewBuffer(body))
	if err != nil {
		fmt.Println("request error:", err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("post error:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		fmt.Printf("report %s failed: %s %s\n", match.ID, resp.Status, bytes.TrimSpace(msg))
		return
	}

	var report ReportResponse
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		fmt.Println("invalid json:", err)
		return
	}
	SimStats.RecordMatch(order[0] == favorite, report.PlayerRatings)

	fmt.Printf("Match %s team ratings: %v\n", match.ID, report.TeamRatings)
}
