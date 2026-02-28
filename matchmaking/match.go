package matchmaking

import (
	"go-match-maker/glicko"
)

type Match struct {
	Player1 *glicko.Player
	Player2 *glicko.Player
}
