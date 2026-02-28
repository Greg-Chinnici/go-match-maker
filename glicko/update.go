package glicko

import "math"

func (p *Player) Update(opponents []*Player, scores []float64) {
	mu := (p.Rating - 1500) / scale
	phi := p.RD / scale

	var vInv float64
	var deltaSum float64

	for i, opp := range opponents {
		muJ := (opp.Rating - 1500) / scale
		phiJ := opp.RD / scale

		EVal := E(mu, muJ, phiJ)
		gVal := g(phiJ)

		vInv += gVal * gVal * EVal * (1 - EVal)
		deltaSum += gVal * (scores[i] - EVal)
	}

	v := 1 / vInv
	//delta := v * deltaSum

	// simplified volatility update
	// make this better later
	newPhi := math.Sqrt(phi*phi + p.Volatility*p.Volatility)

	phiStar := 1 / math.Sqrt(1/(newPhi*newPhi)+1/v)

	mu = mu + phiStar*phiStar*deltaSum

	p.Rating = mu*scale + 1500
	p.RD = phiStar * scale
}
