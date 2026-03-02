# Match Maker

- My attempt at implementing Glicko-2 matchmaking (better alternative to ELO)

- Run the server with:
`go run main.go`

### Improvements in progress
1. Adding better evaluation for matching (avg ping, player role, etc)
2. client side flow (as of now it wont report a player into a match)
    - Ideally the match is still reported server side anyway
3. Make the Queue / Waiting list into a B-Tree
    - sliding window to check to pull X amount of players within rating delta
4. Once players are in a lobby, use a few differnt team balance strategies
    - Higher rated lobbies will balance differently

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
- If you want to change the Rating delta for a valid match just change the value in `queue.ProcessMatches()` in main.go
- run `go run . seed` to add 1000 players into the db
- test with the `bulkQueue.py` maek sure the to set a csv file with a list of player IDs
