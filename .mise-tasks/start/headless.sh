#!/usr/bin/env bash

#MISE description="Start all development processes in headless mode (no TUI)"
#MISE raw=true

set -e

LOG_DIR="/tmp/gram-dev"
mkdir -p "$LOG_DIR"

cleanup() {
  echo ""
  echo "Stopping all processes..."
  kill $(jobs -p) 2>/dev/null
  wait 2>/dev/null
  echo "All processes stopped. Logs in $LOG_DIR/"
}
trap cleanup EXIT INT TERM

echo "Logs: $LOG_DIR/{mock-idp,server,worker,dashboard}.log"
echo ""

echo "Starting mock-idp..."
mise run start:mock-idp > "$LOG_DIR/mock-idp.log" 2>&1 &

echo "Starting server..."
mise run start:server > "$LOG_DIR/server.log" 2>&1 &

echo "Starting worker..."
mise run start:worker > "$LOG_DIR/worker.log" 2>&1 &

echo "Starting dashboard..."
mise run start:dashboard > "$LOG_DIR/dashboard.log" 2>&1 &

echo ""
echo "All processes started. Press Ctrl+C to stop."
echo "Tail logs: tail -f $LOG_DIR/*.log"
wait
