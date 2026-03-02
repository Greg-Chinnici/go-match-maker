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
	server.InitDB(postgresConnStr())
	if len(os.Args) == 2 && os.Args[1] == "seed" {
		server.Seed()
		fmt.Println("Database seeded with 1000 new players")
		return
	}

	queue := matchmaking.NewQueue()

	server.RegisterHandlers(queue)
	fmt.Println("Starting Loop Routine")
	go func() {
		for {
			time.Sleep(1 * time.Second)

			match, ok, matchId := queue.ProcessMatches(100)
			if ok {
				fmt.Printf("Match ID %s\n", matchId)

				fmt.Printf("\t Matched %s %.2f\n",
					match.Player1.ID, match.Player1.Rating,
				)
				fmt.Printf("\t Matched %s %.2f\n",
					match.Player2.ID, match.Player2.Rating,
				)
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
