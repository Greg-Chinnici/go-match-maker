# Match Maker

- My attempt at implementing Glicko-2 matchmaking (better alternative to ELO)

- Quick start (needs Docker running):

```sh
scripts/dev.sh                                  # Postgres + server + load generator
scripts/dev.sh -gamemode BR -lobby 12 -teamCount 4
```

Then open the dashboard at http://localhost:8080/ and start a run from its load generator panel. Ctrl-C stops the server and load generator; the database keeps running.

| Script | |
|---|---|
| `scripts/db.sh [up\|down\|reset\|seed\|psql\|logs]` | local Postgres via docker compose; `up` creates `.env` with a random password if missing, `reset` deletes the data |
| `scripts/server.sh [flags]` | the server and dashboard; starts the database unless `-store memory` |
| `scripts/loadgen.sh [flags]` | no flags: serves the dashboard's load panel on :9090; with flags it runs once, e.g. `-players 500 -rate 100 -duration 1m` |
| `scripts/dev.sh [server flags]` | all of the above together |

- No database or Docker? `scripts/server.sh -store memory` (ratings are lost on exit)
- The scripts build into `bin/`; plain `go run .` and `go run ./cmd/loadgen` work too

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
3. PostgreSQL persistence (pgx), or an in-memory store
4. Dockerized database
5. Read-only dashboard: `html/template` + htmx over server-sent events, charts with Chart.js
6. `cmd/loadgen`: a separate process (and origin) that generates load; the dashboard calls it cross-origin, and it only accepts browser requests from `-allow-origin`

## Dashboard

- `GET /` shows traffic (matches per second, players waiting, queue wait, team balance) and the rating model (rating vs true skill, distributions, correlation, slope, RMSE) for the last ten minutes, plus recent matches
- Every chart has a "Show table" view with the same numbers
- It only reads from the server. Load runs are started from the load generator panel, which `cmd/loadgen` serves from its own origin; `-loadgen ""` hides the panel
- The page loads htmx, its SSE extension, and Chart.js from public CDNs, so it needs internet access

## Load generator

- `go run ./cmd/loadgen -serve :9090` serves the panel and a JSON API: `GET /run` (status), `POST /run` with `{"players": 500, "rate": 100, "duration": "1m"}`, `DELETE /run` (stop)
- It queues players `sim.PlayerID(0..players-1)` round-robin; a player still queued or in a match comes back as busy (409)
- In Docker: `docker compose --profile loadtest up -d` builds and runs it against the server on the host

## Database Setup

- Postgres is the default store. On startup the server pings it (10s timeout) and checks that `glickoplayers` matches `db/schema.sql`, and exits with a hint if either fails
- Local: `docker compose up -d --wait` reads `DB_*` from `.env` (see `.env.example`) and applies `db/schema.sql` the first time the volume is created
- Hosted: set `DATABASE_URL` (it overrides `DB_*`; most hosts want `?sslmode=require`) and apply the schema once with `psql "$DATABASE_URL" -f db/schema.sql`
- `DB_SSLMODE` sets TLS for the `DB_*` connection (default `disable`)

## Usage

| Endpoint | |
|---|---|
| `POST /queue` | `{"uid": "..."}` (optional) queues a player, creating them if new; 409 if already queued or in a match |
| `POST /report` | records a match result (below) |
| `GET /status` | queue size and active matches |
| `GET /active-matches` | matches in progress |
| `GET /ratings` | players currently queued or in a match |
| `GET /sim/stats`, `GET /sim/points` | ratings compared with true skill |
| `GET /events` | the dashboard's live stream (server-sent events) |

1. use the `/queue` endpoint, optionaly include existing `uid` in json body
2. Once players have been matches the server will log the `Match ID` and players involved
3. use the `/report` endpoint to send the result of a match: either `winner_id` (a team ID), or `placements`, a list of team IDs best first. Teams left out of `placements` finished behind every listed team.

```json
{ "match_id": "...", "placements": ["team-1st", "team-2nd", "team-3rd"] }
```

- for now games are automaticallty ended in `mockGameResolver` via a timeout. It plays each match out from every player's hidden "true skill" (derived from a hash of their ID, see `sim/`) and reports the full finishing order
- `GET /sim/stats` shows how close ratings are to those true skills (correlation, slope, RMSE)

## Tests

- `go test ./...` runs unit tests, including the Glicko-2 paper example and a simulation that checks ratings converge to true skill
- `go test ./sim -v` prints the convergence curve for each game mode

---

- If you want to change the Rating delta for a valid match just change the value in `queue.ProcessMatches()` in main.go
- run `go run . seed` to give players 0–999 (the IDs `cmd/loadgen` queues) random established ratings, so a load run starts from ratings that don't match true skill; re-running resets them
- Real world use case change the function in `mockGameResolver.go` in `HandleMatch` to send to actual match server
