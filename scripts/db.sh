#!/usr/bin/env bash
# Local Postgres via docker compose.
#   scripts/db.sh [up]   start it (creates .env with a random password if missing)
#   scripts/db.sh down   stop it, keeping the data
#   scripts/db.sh reset  stop it and delete the data volume
#   scripts/db.sh seed   give players 0-999 random established ratings
#   scripts/db.sh psql   open a psql shell
#   scripts/db.sh logs   follow the database logs
set -euo pipefail
cd "$(dirname "$0")/.."

need_docker() {
  if ! docker info >/dev/null 2>&1; then
    echo "Docker isn't running. Start Docker Desktop (open -a Docker) and try again." >&2
    exit 1
  fi
}

ensure_env() {
  if [ ! -f .env ]; then
    sed "s/^DB_PASSWORD=.*/DB_PASSWORD=$(openssl rand -hex 16)/" .env.example > .env
    echo "Created .env with a random DB_PASSWORD"
  fi
}

case "${1:-up}" in
  up)
    need_docker
    ensure_env
    docker compose up -d --wait db
    ;;
  down)
    need_docker
    docker compose down
    ;;
  reset)
    need_docker
    docker compose down -v
    echo "Deleted the database volume; the next 'up' starts empty and reapplies db/schema.sql"
    ;;
  seed)
    "$0" up
    go build -o bin/server .
    bin/server seed | grep -vE '^(Trying to save a player|SAVE: )'
    ;;
  psql)
    need_docker
    set -a; . ./.env; set +a
    docker compose exec db psql -U "${DB_USER:-postgres}" -d "${DB_NAME:-postgres}"
    ;;
  logs)
    need_docker
    docker compose logs -f db
    ;;
  *)
    sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//' >&2
    exit 2
    ;;
esac
