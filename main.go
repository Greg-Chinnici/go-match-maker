package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"go-match-maker/matchmaking"
	"go-match-maker/server"

	"github.com/joho/godotenv"
)

func main() {
	teamSize := 2
	matchesWaitingSize := 1000
	activeMatchesAtOnce := 20
	ratingDiff := 100.

	server.InitDB(postgresConnStr())
	if len(os.Args) == 2 && os.Args[1] == "seed" {
		server.Seed()
		fmt.Println("Database seeded with 1000 new players.")
		return
	}

	queue := matchmaking.NewQueue()

	server.RegisterHandlers(queue)

	jobs := make(chan *matchmaking.ActiveMatch, matchesWaitingSize)
	server.StartWorkerPool(activeMatchesAtOnce, jobs) // workers will wait if more than X matches are running

	fmt.Println("Starting Loop Routine")
	go func() {
		for {
			time.Sleep(1 * time.Second)

			matches := queue.ProcessMatches(ratingDiff, teamSize)

			for _, match := range matches {
				fmt.Printf("Matched %s vs %s\n",
					match.Team1.TeamUIDSlice(),
					match.Team2.TeamUIDSlice(),
				)

				jobs <- match
			}
		}
	}()

	fmt.Println("Server running on :8080")
	log.Fatal(server.Start(":8080"))

}

func postgresConnStr() string {
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("No .env file found, relying on system env")
	}

	user := os.Getenv("DB_USER")
	pass := os.Getenv("DB_PASSWORD")
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	name := os.Getenv("DB_NAME")

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		user, pass, host, port, name)

	return connStr
}
