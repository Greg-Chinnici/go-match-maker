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

	fmt.Println("Server running on :8080")
	log.Fatal(server.Start(":8080"))

	go func() {
		for {
			time.Sleep(1 * time.Second)

			match, ok := queue.ProcessMatches(100)
			if ok {
				fmt.Printf("Matched %.2f vs %.2f\n",
					match.Player1.Rating,
					match.Player2.Rating,
				)
			}
		}
	}()
}
