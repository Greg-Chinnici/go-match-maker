package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

const QUEUE_API_URL = "http://localhost:8080/queue"

type QueueRequest struct {
	UID string `json:"uid"`
}

type Result struct {
	Timestamp time.Time
	UID       string
	Latency   time.Duration
	Status    int
	Success   bool
}

var client = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     30 * time.Second,
	},
}

func sendPlayer(uid string, results chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done()

	start := time.Now()

	payload := QueueRequest{UID: uid}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", QUEUE_API_URL, bytes.NewBuffer(body))
	if err != nil {
		results <- Result{Timestamp: start, UID: uid, Success: false}
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		results <- Result{Timestamp: start, UID: uid, Latency: latency, Success: false}
		return
	}
	defer resp.Body.Close()

	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	results <- Result{
		Timestamp: start,
		UID:       uid,
		Latency:   latency,
		Status:    resp.StatusCode,
		Success:   success,
	}
}
func loadUIDsFromResults(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("csv has no data")
	}

	var uids []string

	for i := 1; i < len(rows); i++ {
		if len(rows[i]) > 1 {
			uids = append(uids, rows[i][1]) // uid column
		}
	}

	return uids, nil
}
func main() {

	count := flag.Int("count", 1000, "number of players")
	workers := flag.Int("workers", 50, "concurrent workers")
	reuse := flag.String("reuse", "", "path to results.csv to reuse player ids")
	flag.Parse()

	var players []string
	var err error
	if *reuse != "" {
		players, err = loadUIDsFromResults(*reuse)
		if err != nil {
			panic(err)
		}
		fmt.Println("Reusing", len(players), "player IDs from", *reuse)
	} else {
		for i := 0; i < *count; i++ {
			players = append(players, uuid.NewString())
		}
	}

	results := make(chan Result, len(players))
	jobs := make(chan string, len(players))

	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < *workers; i++ {
		go func() {
			for uid := range jobs {
				wg.Add(1)
				sendPlayer(uid, results, &wg)
			}
		}()
	}

	startTime := time.Now()

	for _, p := range players {
		jobs <- p
	}
	close(jobs)

	wg.Wait()
	close(results)

	duration := time.Since(startTime)

	file, err := os.Create("results.csv")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"timestamp", "uid", "latency_ms", "status", "success"})

	var total, success int
	var totalLatency time.Duration

	for r := range results {
		total++
		if r.Success {
			success++
		}
		totalLatency += r.Latency

		writer.Write([]string{
			r.Timestamp.Format(time.RFC3339Nano),
			r.UID,
			fmt.Sprintf("%.3f", float64(r.Latency.Microseconds())/1000.0),
			fmt.Sprintf("%d", r.Status),
			fmt.Sprintf("%t", r.Success),
		})
	}

	fmt.Println("---- LOAD TEST COMPLETE ----")
	fmt.Println("Total Requests:", total)
	fmt.Println("Success:", success)
	fmt.Println("Duration:", duration)
	fmt.Println("Requests/sec:", float64(total)/duration.Seconds())

	if total > 0 {
		fmt.Println("Average Latency (ms):",
			float64(totalLatency.Milliseconds())/float64(total))
	}

	fmt.Println("CSV exported to results.csv")
}
