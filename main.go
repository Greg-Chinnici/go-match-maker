package main

import (
	"fmt"
	"log"
	"time"

	"go-match-maker/matchmaking"
	"go-match-maker/server"
)

func main() {
	queue := matchmaking.NewQueue()

	server.RegisterHandlers(queue)
	fmt.Println("Starting Loop Routine")
	go func() {
		for {
			time.Sleep(1 * time.Second)

			match, ok, matchId := queue.ProcessMatches(100)
			if ok {
				fmt.Printf("Matched %.s vs %.2f\n",
					match.Player1.ID, match.Player1.Rating,
				)
				fmt.Printf("Matched %.s vs %.2f\n",
					match.Player2.ID, match.Player2.Rating,
				)
				fmt.Printf("Match ID %s\n", matchId)
			}
		}
	}()

	fmt.Println("Server running on :8080")
	log.Fatal(server.Start(":8080"))

}
