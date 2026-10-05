#!/usr/bin/env bash
# The load generator. With no flags it serves the dashboard's load panel on
# :9090; with flags it runs once from the command line, e.g.:
#   scripts/loadgen.sh
#   scripts/loadgen.sh -players 500 -rate 100 -duration 1m
set -euo pipefail
cd "$(dirname "$0")/.."

go build -o bin/loadgen ./cmd/loadgen
if [ $# -eq 0 ]; then
  exec bin/loadgen -serve :9090
fi
exec bin/loadgen "$@"
