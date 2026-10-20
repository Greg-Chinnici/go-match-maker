package server

import (
	"go-match-maker/glicko"
	"go-match-maker/sim"
	"math/rand"
)

// Seed upserts players sim.PlayerID(0..amount-1) with random established
// ratings. These are the same IDs cmd/loadgen queues, so a load run starts
// from ratings that don't yet match anyone's true skill. Re-running resets them.
func Seed(amount int) {
	if amount == 0 {
		amount = 1000
	}

	// populate with amount of people
	for i := range amount {
		rating := rand.NormFloat64()*300 + 1500
		rd := 30 + rand.Float64()*320
		vol := 0.03 + rand.Float64()*0.06

		if rating < 1000 {
			rating = 1000
		}
		if rating > 3000 {
			rating = 3000
		}

		p := glicko.EstablishedPlayer(rating, rd, vol, sim.PlayerID(i))

		if err := SavePlayer(p); err != nil {
			panic(err)
		}
	}
}
