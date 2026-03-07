package server

import (
	"context"
	"errors"
	"fmt"
	"go-match-maker/glicko"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct {
	connString string
}
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

var DB *pgxpool.Pool // using global for now

func InitDB(connStr string) error {
	var err error
	DB, err = pgxpool.New(context.Background(), connStr)

	if err != nil {
		return fmt.Errorf("Unable to connect to database: %v\n", err)
	}

	err = DB.Ping(context.Background())
	if err != nil {
		return fmt.Errorf("Ping failed: %v\n", err)
	}
	return nil
}

func TryFetchPlayer(uuid string) (*glicko.Player, error) {
	var playerData glicko.Player
	query := "SELECT rating , rd , volatility, id FROM glickoplayers WHERE id = $1"
	err := DB.QueryRow(context.Background(), query, uuid).
		Scan(
			&playerData.Rating,
			&playerData.RD,
			&playerData.Volatility,
			&playerData.ID)

	playerData.AvgPing = 50

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &playerData, err
}
func SavePlayer(player *glicko.Player) error {
	return savePlayer(context.Background(), DB, player)
}
func SavePlayersInTx(players []*glicko.Player) error {
	ctx := context.Background()

	tx, err := DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // safe even if committed

	for _, p := range players {
		if err := savePlayer(ctx, tx, p); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
func savePlayer(ctx context.Context, db DBTX, player *glicko.Player) error {
	fmt.Println("Trying to save a player")
	fmt.Print("SAVE: ")
	fmt.Println(player)

	insert := `
		INSERT INTO glickoplayers (id, rating, rd, volatility)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id)
		DO UPDATE SET
			rating = EXCLUDED.rating,
			rd = EXCLUDED.rd,
			volatility = EXCLUDED.volatility
	`
	_, err := db.Exec(ctx, insert,
		player.ID, player.Rating, player.RD, player.Volatility,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			fmt.Printf("Postgres error code: %s", pgErr.Code)
			fmt.Printf("Message: %s", pgErr.Message)
			fmt.Printf("Detail: %s", pgErr.Detail)
		} else {
			fmt.Printf("Non-PG error: %+v", err)
		}
	}

	return err

}
