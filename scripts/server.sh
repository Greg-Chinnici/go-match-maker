#!/usr/bin/env bash
# The match maker server and dashboard (http://localhost:8080/). Flags pass
# through, e.g.:
#   scripts/server.sh -gamemode BR -lobby 12 -teamCount 4
#   scripts/server.sh -store memory      (no database needed)
set -euo pipefail
cd "$(dirname "$0")/.."

case " $* " in
  *" -store memory "* | *" -store=memory "* | *" --store memory "* | *" --store=memory "*) ;;
  *) scripts/db.sh up ;;
esac

go build -o bin/server .
exec bin/server "$@"
