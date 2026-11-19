#!/usr/bin/env bash
# ask-watch v1: wait for an approval decision on one ask.
# Prints one line per status change and exits once the ask is decided.
# usage: ask-watch-v1.sh <status-url> [interval-secs] [max-ticks]
set -uo pipefail
url="$1"
interval="${2:-3}"
ticks="${3:-1200}"
last=""
n=0
while [ "$n" -lt "$ticks" ]; do
  n=$((n + 1))
  status=$(curl -fsS --max-time 5 "$url" 2>/dev/null || echo "unreachable")
  if [ "$status" != "$last" ]; then
    echo "ASK STATUS: $status"
    last="$status"
  fi
  case "$status" in
    approved)
      echo "ASK APPROVED: retry the blocked tool call now with exactly the same arguments"
      exit 0
      ;;
    denied)
      echo "ASK DENIED: do not retry; tell the user"
      exit 0
      ;;
    expired)
      echo "ASK EXPIRED: nobody decided in time; tell the user"
      exit 0
      ;;
  esac
  sleep "$interval"
done
echo "ASK WATCH TIMEOUT after $ticks checks"
