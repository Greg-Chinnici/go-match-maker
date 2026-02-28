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
		Rating:     DefaultRating,
		RD:         DefaultRD,
		Volatility: DefaultVolatility,

		AvgPing: 50,
	}
}
func (p *Player) snapshot() *Player {
	return &Player{
		Rating:     p.Rating,
		RD:         p.RD,
		Volatility: p.Volatility,
	}
}
func EstablishedPlayer(rating, deviation, volatility float64, id string) *Player {
	return &Player{
		ID:         id,
		Rating:     rating,
		RD:         deviation,
		Volatility: volatility,

		AvgPing: 50,
	}
}

// Make a contructor to pull from a DB given a UUID
