package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"go-match-maker/matchmaking"
)

const (
	REPORT_API_URL = "http://localhost:8080/report"
)

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
func HandleMatch(match *matchmaking.ActiveMatch) {
	time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond)

	var winner string
	if rand.Intn(2) == 0 {
		winner = match.Team1.ID
	} else {
		winner = match.Team2.ID
	}

	payload := ReportRequest{
		Winner:  winner,
		MatchID: match.ID,
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
		fmt.Println("post error: ", err)
		return
	}

	defer resp.Body.Close()
	fmt.Println(resp.Body)
}
