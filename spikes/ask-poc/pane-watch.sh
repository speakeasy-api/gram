#!/usr/bin/env bash
# pane-watch: report Claude Code TUI state changes in a tmux pane, one line each.
# usage: pane-watch.sh <tmux-target> [max-seconds]
# Lines: STATE PERMISSION | STATE TRUST | STATE BUSY | STATE IDLE | SENT | PANE GONE | WATCH TIMEOUT
set -uo pipefail
target="$1"
limit="${2:-900}"
last=""
sent=0
n=0
while [ "$n" -lt "$limit" ]; do
  n=$((n + 1))
  pane=$(tmux capture-pane -p -t "$target" 2>/dev/null) || {
    echo "PANE GONE"
    exit 0
  }
  if echo "$pane" | grep -qiE "trust the files|Yes, I trust|trust this folder"; then
    state="TRUST"
  elif echo "$pane" | grep -qE "Do you want to|Would you like to|1\. Yes"; then
    state="PERMISSION"
  elif echo "$pane" | grep -qiE "esc to interrupt"; then
    state="BUSY"
  else
    state="IDLE"
  fi
  if [ "$state" != "$last" ]; then
    echo "STATE $state"
    last="$state"
  fi
  if [ "$sent" -eq 0 ] && echo "$pane" | grep -qE "Message sent to"; then
    echo "SENT"
    sent=1
  fi
  sleep 1
done
echo "WATCH TIMEOUT"
