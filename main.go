package main

import (
	"flag"
	"fmt"
	"log"
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
	flag.IntVar(&teamCount, "teamCount", 2, "Total Teams per Match")
	flag.IntVar(&lobbySize, "lobby", 2, "Total Players in each Match")
	flag.StringVar(&gameType, "gamemode", "FFA", "FFA , BR , TDM")
	flag.Float64Var(&ratingDiff, "ratingDiff", 100., "Each Lobby's max rating delta")

	flag.Parse()

	args := flag.Args()

	err := server.InitDB(postgresConnStr())
	if err != nil {
		fmt.Println(err)
		return
	}

	if len(args) > 0 && args[0] == "seed" {
		server.Seed()
		fmt.Println("Database seeded with 1000 new players.")
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

			for _, match := range matches {
				fmt.Printf("Teams in Match %s\n", match.ID)
				for _, team := range match.Teams {
					fmt.Printf("\t %s\n", team.ID)
				}

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
