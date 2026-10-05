package server

import (
	"context"
	"errors"
	"fmt"
	"go-match-maker/glicko"
	"slices"
	"strings"
	"time"

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

// PlayerStore persists player ratings. Handlers go through store so tests can
// swap in a fake instead of Postgres.
type PlayerStore interface {
	FetchPlayer(ctx context.Context, id string) (*glicko.Player, error)
	SavePlayers(ctx context.Context, players []*glicko.Player) error
}

var store PlayerStore = postgresStore{}

type postgresStore struct{}

func (postgresStore) FetchPlayer(ctx context.Context, id string) (*glicko.Player, error) {
	return fetchPlayer(ctx, id)
}

func (postgresStore) SavePlayers(ctx context.Context, players []*glicko.Player) error {
	return savePlayersInTx(ctx, players)
}

func InitDB(connStr string) error {
	var err error
	DB, err = pgxpool.New(context.Background(), connStr)

	if err != nil {
		return fmt.Errorf("Unable to connect to database: %v", err)
	}

	// bounded so an unreachable hosted database fails fast at startup
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = DB.Ping(ctx)
	if err != nil {
		return fmt.Errorf("Ping failed: %v", err)
	}
	return nil
}

// requiredColumns is the glickoplayers schema the server reads and writes,
// matching db/schema.sql.
var requiredColumns = map[string]string{
	"id":         "uuid",
	"rating":     "double precision",
	"rd":         "double precision",
	"volatility": "double precision",
}

// CheckSchema confirms the glickoplayers table exists with the columns the
// server needs, so a missing migration fails at startup instead of on the
// first request.
func CheckSchema(ctx context.Context) error {
	rows, err := DB.Query(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'glickoplayers'`)
	if err != nil {
		return fmt.Errorf("reading schema: %w", err)
	}

	found := map[string]string{}
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return fmt.Errorf("reading schema: %w", err)
		}
		found[name] = dataType
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading schema: %w", err)
	}

	return compareSchema(found)
}

// compareSchema reports every way the found columns differ from requiredColumns.
func compareSchema(found map[string]string) error {
	if len(found) == 0 {
		return errors.New("table glickoplayers not found: start the database with `docker compose up -d --wait` or apply db/schema.sql")
	}

	var problems []string
	for name, want := range requiredColumns {
		got, ok := found[name]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("missing column %s (%s)", name, want))
		case got != want:
			problems = append(problems, fmt.Sprintf("column %s is %s, want %s", name, got, want))
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return fmt.Errorf("table glickoplayers does not match db/schema.sql: %s", strings.Join(problems, "; "))
	}
	return nil
}

func TryFetchPlayer(uuid string) (*glicko.Player, error) {
	return fetchPlayer(context.Background(), uuid)
}
func fetchPlayer(ctx context.Context, uuid string) (*glicko.Player, error) {
	var playerData glicko.Player
	query := "SELECT rating , rd , volatility, id FROM glickoplayers WHERE id = $1"
	err := DB.QueryRow(ctx, query, uuid).
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
	return savePlayersInTx(context.Background(), players)
}
func savePlayersInTx(ctx context.Context, players []*glicko.Player) error {
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
