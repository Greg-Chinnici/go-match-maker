package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"go-match-maker/matchmaking"
	"go-match-maker/server"

	"github.com/joho/godotenv"
)

func main() {
	matchesWaitingSize := 1000
	activeMatchesAtOnce := 40

	var teamCount int
	var lobbySize int
	var gameType string
	var ratingDiff float64
	var storeKind string
	var loadgenURL string
	flag.IntVar(&teamCount, "teamCount", 2, "Total Teams per Match")
	flag.IntVar(&lobbySize, "lobby", 2, "Total Players in each Match")
	flag.StringVar(&gameType, "gamemode", "FFA", "FFA , BR , TDM")
	flag.Float64Var(&ratingDiff, "ratingDiff", 100., "Each Lobby's max rating delta")
	flag.StringVar(&storeKind, "store", "postgres", "postgres , memory (ratings are lost on exit)")
	flag.StringVar(&loadgenURL, "loadgen", "http://localhost:9090", "load generator the dashboard connects to; empty hides the panel")

	flag.Parse()

	args := flag.Args()

	seeding := len(args) > 0 && args[0] == "seed"

	switch storeKind {
	case "postgres":
		if err := connectPostgres(); err != nil {
			fmt.Println(err)
			return
		}
	case "memory":
		if seeding {
			fmt.Println("seed needs -store postgres; an in-memory store is empty on every start")
			return
		}
		fmt.Println("Using in-memory store: ratings are lost when the server stops")
		server.UseStore(server.NewMemoryStore())
	default:
		fmt.Printf("unknown -store %q, use postgres or memory\n", storeKind)
		return
	}

	if seeding {
		server.Seed(1000)
		fmt.Println("Seeded players 0-999 (the IDs cmd/loadgen queues).")
		return
	}

	config, err := matchmaking.ConfigFactory(gameType, lobbySize, teamCount)
	if err != nil {
		fmt.Println(err)
		fmt.Println("Use the '-h' flag to see all options")
		return
	}
	queue := matchmaking.NewQueue()

	server.RegisterHandlers(queue)
	server.StartTelemetry(queue, server.Info{
		Mode:       gameType,
		Lobby:      config.LobbySize,
		Teams:      config.TeamCount,
		RatingDiff: ratingDiff,
		Store:      storeKind,
		LoadgenURL: loadgenURL,
	})

	jobs := make(chan *matchmaking.ActiveMatch, matchesWaitingSize)
	server.StartWorkerPool(activeMatchesAtOnce, jobs) // workers will wait if more than workerCount matches are running

	fmt.Println("Starting Loop Routine")
	go func() {
		for {
			time.Sleep(1 * time.Second)

			matches, err := queue.ProcessMatches(ratingDiff, config)
			if err != nil {
				fmt.Println(err)
				continue
			}
			server.RecordMatchesCreated(matches)

			for _, match := range matches {
				fmt.Printf("Teams in Match %s\n", match.ID)
				for _, team := range match.Teams {
					fmt.Printf("\t %s\n", team.ID)
				}

				jobs <- match
			}
		}
	}()

	fmt.Println("Server running on :8080, dashboard at http://localhost:8080/")
	log.Fatal(server.Start(":8080"))

}

// connectPostgres connects, checks the schema, and explains what to do when
// either fails.
func connectPostgres() error {
	connStr, err := postgresConnStr()
	if err != nil {
		return err
	}
	target := connStr
	if u, err := url.Parse(connStr); err == nil {
		target = u.Redacted()
	}
	fmt.Println("Connecting to", target)

	if err := server.InitDB(connStr); err != nil {
		return fmt.Errorf("%w\nStart a local database with `docker compose up -d --wait`, check DATABASE_URL or DB_* in .env, or run with -store memory", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.CheckSchema(ctx)
}

// postgresConnStr uses DATABASE_URL when set (the usual form for hosted
// databases), otherwise builds a URL from DB_* variables.
func postgresConnStr() (string, error) {
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("No .env file found, relying on system env")
	}

	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		return dbURL, nil
	}

	user := os.Getenv("DB_USER")
	pass := os.Getenv("DB_PASSWORD")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	name := os.Getenv("DB_NAME")
	sslMode := os.Getenv("DB_SSLMODE")
	if sslMode == "" {
		sslMode = "disable"
	}
	if host == "" || user == "" {
		return "", fmt.Errorf("no database configured: set DATABASE_URL or DB_* in .env (see .env.example), or run with -store memory")
	}

	// url.URL escapes credentials, so passwords containing @ / : still work
	connURL := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, pass),
		Host:     net.JoinHostPort(host, port),
		Path:     name,
		RawQuery: url.Values{"sslmode": {sslMode}}.Encode(),
	}

	return connURL.String(), nil
}
