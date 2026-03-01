package server

import (
	"context"
	"errors"
	"fmt"
	"go-match-maker/glicko"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct {
	connString string
}

var DB *pgxpool.Pool

func InitDB(connStr string) {
	var err error
	DB, err = pgxpool.New(context.Background(), connStr)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	err = DB.Ping(context.Background())
	if err != nil {
		fmt.Printf("Ping failed: %v\n", err)
	}

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
		return nil, fmt.Errorf("player not found")
	}
	if err != nil {
		return nil, err
	}

	return &playerData, err
}

func SavePlayer(player *glicko.Player) error {
	fmt.Println("Trying to save a player")
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
	_, err := DB.Exec(context.Background(), insert,
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
