package glicko

import "github.com/google/uuid"

type Player struct {
	ID         string
	Rating     float64
	RD         float64 // rating deviation
	Volatility float64

	AvgPing float64 //ms
}

func NewPlayer() *Player {
	return &Player{
		ID:         uuid.New().String(),
		Rating:     1500,
		RD:         350,
		Volatility: 0.06,

		AvgPing: 50,
	}
}
