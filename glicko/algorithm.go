package glicko

import "math"

const (
	DefaultRating     = 1500.0
	DefaultRD         = 350.0
	DefaultVolatility = 0.06
	DefaultTau        = 0.5

	glickoScale = 173.7178
)

type Result struct {
	Opponent *Player
	Score    float64 // 1.0 = win, 0.5 = draw, 0.0 = loss
}

func (p *Player) mu() float64 {
	return (p.Rating - 1500.0) / glickoScale
}

func (p *Player) phi() float64 {
	return p.RD / glickoScale
}

func (p *Player) g() float64 {
	phi := p.phi()
	return 1.0 / math.Sqrt(1.0+3.0*phi*phi/(math.Pi*math.Pi))
}

func (p *Player) e(opponent *Player) float64 {
	return 1.0 / (1.0 + math.Exp(-opponent.g()*(p.mu()-opponent.mu())))
}

func (p *Player) v(results []Result) float64 {
	sum := 0.0
	for _, r := range results {
		gJ := r.Opponent.g()
		eJ := p.e(r.Opponent)
		sum += gJ * gJ * eJ * (1.0 - eJ)
	}
	return 1.0 / sum
}

func (p *Player) outcomeSum(results []Result) float64 {
	sum := 0.0
	for _, r := range results {
		sum += r.Opponent.g() * (r.Score - p.e(r.Opponent))
	}
	return sum
}

func (p *Player) delta(results []Result, v float64) float64 {
	return v * p.outcomeSum(results)
}

// illinois algorithm to compute the new volatility.
func (p *Player) newVolatility(v, delta, tau float64) float64 {
	const epsilon = 1e-6

	phi := p.phi()
	sigma := p.Volatility
	phiSq := phi * phi
	deltaSq := delta * delta

	f := func(x float64) float64 {
		ex := math.Exp(x)
		denom := phiSq + v + ex
		return ex*(deltaSq-phiSq-v-ex)/(2.0*denom*denom) - (x-math.Log(sigma*sigma))/(tau*tau)
	}

	a := math.Log(sigma * sigma)
	b := func() float64 {
		if deltaSq > phiSq+v {
			return math.Log(deltaSq - phiSq - v)
		}
		k := 1.0
		for f(a-k*tau) < 0 {
			k++
		}
		return a - k*tau
	}()

	fA, fB := f(a), f(b)
	for math.Abs(b-a) > epsilon {
		c := a + (a-b)*fA/(fB-fA)
		fC := f(c)
		if fC*fB < 0 {
			a, fA = b, fB
		} else {
			fA /= 2.0
		}
		b, fB = c, fC
	}

	return math.Exp(a / 2.0)
}

// update computes and applies new ratings
func (p *Player) update(results []Result, tau float64) {
	phi := p.phi()
	sigma := p.Volatility

	if len(results) == 0 {
		// Inactive period: only RD increases.
		p.RD = glickoScale * math.Sqrt(phi*phi+sigma*sigma)
		return
	}

	v := p.v(results)
	delta := p.delta(results, v)
	sigmaPrime := p.newVolatility(v, delta, tau)

	phiStar := math.Sqrt(phi*phi + sigmaPrime*sigmaPrime)
	phiPrime := 1.0 / math.Sqrt(1.0/(phiStar*phiStar)+1.0/v)
	muPrime := p.mu() + phiPrime*phiPrime*p.outcomeSum(results)

	p.Rating = glickoScale*muPrime + 1500.0
	p.RD = glickoScale * phiPrime
	p.Volatility = sigmaPrime
}

func (p *Player) Update(opponents []*Player, scores []float64) {
	p.UpdateWithTau(opponents, scores, DefaultTau)
}

func (p *Player) UpdateWithTau(opponents []*Player, scores []float64, tau float64) {
	results := make([]Result, len(opponents))
	for i, opp := range opponents {
		results[i] = Result{
			Opponent: opp.snapshot(), // use pre-period ratings
			Score:    scores[i],
		}
	}
	p.update(results, tau)
}

// winner must be p1 or p2; any other value records a draw.
func UpdateMatch(p1, p2 *Player, winner *Player) {
	UpdateMatchWithTau(p1, p2, winner, DefaultTau)
}

func UpdateMatchWithTau(p1, p2 *Player, winner *Player, tau float64) {
	snap1 := p1.snapshot()
	snap2 := p2.snapshot()

	var s1, s2 float64
	switch winner {
	case p1:
		s1, s2 = 1.0, 0.0
	case p2:
		s1, s2 = 0.0, 1.0
	default:
		s1, s2 = 0.5, 0.5
	}

	p1.update([]Result{{Opponent: snap2, Score: s1}}, tau)
	p2.update([]Result{{Opponent: snap1, Score: s2}}, tau)
}

func (p *Player) ExpectedScore(opponent *Player) float64 {
	return p.e(opponent)
}
