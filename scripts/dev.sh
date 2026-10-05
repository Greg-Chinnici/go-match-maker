#!/usr/bin/env bash
# Everything at once: Postgres, the match maker and the load generator. Flags
# go to the server (same as scripts/server.sh). Ctrl-C stops the server and
# load generator; the database keeps running (scripts/db.sh down stops it).
set -euo pipefail
cd "$(dirname "$0")/.."

case " $* " in
  *" -store memory "* | *" -store=memory "* | *" --store memory "* | *" --store=memory "*) ;;
  *) scripts/db.sh up ;;
esac

go build -o bin/server .
go build -o bin/loadgen ./cmd/loadgen

bin/loadgen -serve :9090 &
loadgen=$!
bin/server "$@" &
server=$!
trap 'kill "$server" "$loadgen" 2>/dev/null || true; wait 2>/dev/null || true' EXIT
trap 'exit 130' INT TERM

echo "Dashboard: http://localhost:8080/  (Ctrl-C to stop)"

# stop both as soon as either exits, e.g. the server failing its startup checks
while kill -0 "$server" 2>/dev/null && kill -0 "$loadgen" 2>/dev/null; do
  sleep 1
done
