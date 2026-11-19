#!/usr/bin/env bash
# run-watch: one line per Ask POC server event that matters, plus agent pane
# busy/idle changes and a few TUI prompts. Works for Claude Code and Codex.
# usage: run-watch.sh <tmux-target> [max-seconds]
set -uo pipefail
target="$1"
limit="${2:-900}"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG="$DIR/.run/server.log"
seen=$(wc -l <"$LOG" 2>/dev/null || echo 0)
last=""
n=0
while [ "$n" -lt "$limit" ]; do
  n=$((n + 1))
  total=$(wc -l <"$LOG" 2>/dev/null || echo 0)
  if [ "$total" -gt "$seen" ]; then
    tail -n +"$((seen + 1))" "$LOG" | grep -E '"event":"(slack\.blocked|slack\.blocked_pending|ask\.created|ask\.approved|ask\.denied|ask\.expired|slack\.sent)"' | sed -E 's/.*"t":"([^"]+)".*"event":"([^"]+)".*/SERVER \2 at \1/'
    seen="$total"
  fi
  pane=$(tmux capture-pane -p -t "$target" 2>/dev/null) || {
    echo "PANE GONE"
    exit 0
  }
  if echo "$pane" | grep -qiE "trust (the files|this|the contents)|Do you trust|allow Codex to work"; then
    state="TRUST PROMPT"
  elif echo "$pane" | grep -qE "Do you want to|Would you like to|1\. Yes|Allow command|Approve"; then
    state="PERMISSION PROMPT"
  elif echo "$pane" | grep -qiE "esc to interrupt"; then
    state="BUSY"
  else
    state="IDLE"
  fi
  if [ "$state" != "$last" ]; then
    echo "PANE $state"
    last="$state"
  fi
  sleep 1
done
echo "WATCH TIMEOUT"
