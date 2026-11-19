#!/usr/bin/env bash
# Control the Ask POC MCP servers (mock external Slack + approvals).
#
#   ./ask-poc.sh restart        stop anything running, then start fresh (empty state)
#   ./ask-poc.sh start | stop | status | logs
#   ./ask-poc.sh approve <ask_id> | deny <ask_id>   decide by hand
#   ./ask-poc.sh reset          clear blocked calls, asks and sent messages
#
# Env: ASK_POC_AUTO_APPROVE_SECONDS (default 10, 0 = decide by hand),
#      ASK_POC_ERROR_STYLE (tool | jsonrpc), ASK_POC_TTL_SECONDS (default 1800).
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_DIR="$DIR/.run"
PID_FILE="$RUN_DIR/server.pid"
LOG_FILE="$RUN_DIR/server.log"
SLACK_PORT="${ASK_POC_SLACK_PORT:-4701}"
APPROVALS_PORT="${ASK_POC_APPROVALS_PORT:-4702}"
APPROVALS="http://127.0.0.1:$APPROVALS_PORT"

stop() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    kill "$(cat "$PID_FILE")" 2>/dev/null || true
  fi
  rm -f "$PID_FILE"
  for port in "$SLACK_PORT" "$APPROVALS_PORT"; do
    for pid in $(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null); do
      kill "$pid" 2>/dev/null || true
    done
  done
  sleep 0.3
  echo "stopped"
}

start() {
  mkdir -p "$RUN_DIR"
  if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "already running (pid $(cat "$PID_FILE")); use restart"
    return 0
  fi
  : >"$LOG_FILE"
  nohup node "$DIR/server.mjs" >>"$LOG_FILE" 2>&1 &
  echo $! >"$PID_FILE"
  for _ in $(seq 1 30); do
    if curl -fsS "$APPROVALS/health" >/dev/null 2>&1 && curl -fsS "http://127.0.0.1:$SLACK_PORT/health" >/dev/null 2>&1; then
      echo "running (pid $(cat "$PID_FILE"))"
      echo "  external-slack: http://127.0.0.1:$SLACK_PORT/mcp"
      echo "  approvals:      $APPROVALS/mcp"
      echo "  log:            $LOG_FILE"
      return 0
    fi
    sleep 0.2
  done
  echo "failed to start; last log lines:" >&2
  tail -n 20 "$LOG_FILE" >&2
  return 1
}

case "${1:-help}" in
  start) start ;;
  stop) stop ;;
  restart) stop && start ;;
  status) curl -fsS "$APPROVALS/health" && echo ;;
  logs) tail -n 200 -f "$LOG_FILE" ;;
  approve) curl -fsS -X POST "$APPROVALS/asks/${2:?ask id}/approve" ;;
  deny) curl -fsS -X POST "$APPROVALS/asks/${2:?ask id}/deny" ;;
  reset) curl -fsS -X POST "$APPROVALS/reset" ;;
  *) sed -n '2,12p' "$0" ;;
esac
