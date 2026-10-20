// Command loadgen queues simulated players against a match maker server.
//
// One run from the command line:
//
//	go run ./cmd/loadgen -players 500 -rate 100 -duration 1m
//
// Or serve a control API and the panel the dashboard shows:
//
//	go run ./cmd/loadgen -serve :9090
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"
)

func main() {
	target := flag.String("target", "http://localhost:8080", "match maker server")
	serve := flag.String("serve", "", "address to serve the control API on, e.g. :9090; empty runs once and exits")
	origins := flag.String("allow-origin", "http://localhost:8080,http://127.0.0.1:8080", "comma-separated browser origins allowed to call the control API")
	players := flag.Int("players", defaultConfig.Players, "simulated players, reused round-robin")
	rate := flag.Float64("rate", defaultConfig.Rate, "queue requests per second")
	duration := flag.Duration("duration", defaultConfig.Duration, "how long to run")
	flag.Parse()

	runner := NewRunner(*target)

	if *serve != "" {
		allowed := strings.Split(*origins, ",")
		fmt.Printf("loadgen control API on %s, targeting %s, allowing %v\n", *serve, *target, allowed)
		log.Fatal(http.ListenAndServe(*serve, newHandler(runner, allowed)))
	}

	cfg := Config{Players: *players, Rate: *rate, Duration: *duration}
	if err := runner.Start(cfg); err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	fmt.Printf("Queueing %d players at %g/s for %s against %s (Ctrl-C stops)\n", cfg.Players, cfg.Rate, cfg.Duration, *target)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		runner.Stop()
	}()

	done := make(chan struct{})
	go func() {
		runner.Wait()
		close(done)
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			printStatus(runner.Status())
		case <-done:
			printStatus(runner.Status())
			if s := runner.Status(); s.LastError != "" {
				fmt.Println("last error:", s.LastError)
			}
			return
		}
	}
}

func printStatus(s Status) {
	fmt.Printf("%-7s %4.0fs  sent %d  ok %d  busy %d  failed %d  %.0f/s  p50 %.1fms  p95 %.1fms  p99 %.1fms\n",
		s.State, s.ElapsedS, s.Sent, s.OK, s.Busy, s.Failed, s.RPS, s.P50Ms, s.P95Ms, s.P99Ms)
}
