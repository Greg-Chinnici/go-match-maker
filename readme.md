# Match Maker

- My attempt at implementing Glicko-2 matchmaking (better alternative to ELO)

- Run the server with:
  `go run main.go -gamemode BR -lobby 24 -teamCount 6`

### Improvements in progress

1. Adding better evaluation for matching (avg ping, player role, etc)
2. Once players are in a lobby, use a few different team balance strategies

- [x] Random Team
- [x] Snake Draft
- [x] Greedy Draft
- [x] FFA

## Architecure

1. HTTP API (net/http)
2. Glicko-2 rating engine (in-memory)
3. PostgreSQL persistence (pgx)
4. Dockerized database

## Bruno as an API client

- Check the APIs in /bruno/GlickoServer

## Database Setup

- Hosting a Postgres db on docker connecting with a `.env`

```env
DB_PASSWORD=yourpassword
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_NAME=postgres
```

### Tables

```sql
CREATE TABLE public.glickoplayers (
  id uuid PRIMARY KEY,
  rating double precision NOT NULL,
  rd double precision NOT NULL,
  volatility double precision NOT NULL
);
```

## Usage

1. use the `/queue` endpoint, optionaly include existing `uid` in json body
2. Once players have been matches the server will log the `Match ID` and players involved
3. use the `/report` endpoint to send in which player Won a certain match

- for now games are automaticallty ended in `mockGameResolver` via a timeout

---

- If you want to change the Rating delta for a valid match just change the value in `queue.ProcessMatches()` in main.go
- run `go run . seed` to add 1000 players into the db (not needed but set a normally distributed group of players)
- test with the `BulkTests/main.go`
- Real world use case change the function in `mockGameResolver.go` in `HandleMatch` to send to actual match server
