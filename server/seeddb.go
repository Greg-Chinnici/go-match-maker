package server

import (
	"encoding/csv"
	"go-match-maker/glicko"
	"math/rand"
	"os"
	"time"

	"github.com/google/uuid"
)

func Seed(amount int) {
	file, err := os.Create("results.csv")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if amount == 0 {
		amount = 1000
	}

	// populate with amount of people
	for range amount {
		id := uuid.New().String()

		rating := rand.NormFloat64()*300 + 1500
		rd := 30 + rand.Float64()*320
		vol := 0.03 + rand.Float64()*0.06

		if rating < 1000 {
			rating = 1000
		}
		if rating > 3000 {
			rating = 3000
		}

		p := glicko.EstablishedPlayer(rating, rd, vol, id)

		err := SavePlayer(p)

		writer.Write([]string{
			time.Now().Format(time.RFC3339Nano),
			p.ID,
		})

		if err != nil {
			panic(err)
		}
	}
}
