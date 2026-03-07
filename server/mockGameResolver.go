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
	// Real world case would be to send this to the actual match servers
	// then tell the match how to report a finished game
	time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond)

	winner := match.Teams[rand.Intn(len(match.Teams))].ID
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
		fmt.Println("post error:", err)
		return
	}
	defer resp.Body.Close()

	bodyResp, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("read error:", err)
		return
	}

	fmt.Println("Status:", resp.Status)
	fmt.Println("Response JSON:")
	var pretty bytes.Buffer
	err = json.Indent(&pretty, bodyResp, "", "  ")
	if err != nil {
		fmt.Println("invalid json:", err)
		return
	}

	fmt.Println(pretty.String())
}
