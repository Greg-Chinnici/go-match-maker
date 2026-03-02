package server

import (
	"math/rand"

	"github.com/google/uuid"

	"go-match-maker/glicko"
)

func Seed() {

	// populate with 1000 people
	for range 1000 {
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

		if err != nil {
			panic(err)
		}
	}
}
