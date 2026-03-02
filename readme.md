# Match Maker

- My attempt at implementing Glicko-2 matchmaking (better alternative to ELO)

- Run the server with:
`go run main.go`

### Improvements in progress
1. Adding better evaluation for matching (avg ping, player role, etc)
2. client side flow (as of now it wont report a player into a match)
    - Ideally the match is still reported server side anyway

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