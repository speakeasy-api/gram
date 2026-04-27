#!/usr/bin/env bash

#MISE description="Kill processes listening on development ports (app services only)"

# Application service ports (non-Docker)
PORTS=(
  "8080:gram-server"
  "8081:gram-control"
  "8082:gram-worker-control"
  "35291:mock-idp"
  "5173:dashboard"
  "6007:elements-storybook"
)

killed=0
for entry in "${PORTS[@]}"; do
  port="${entry%%:*}"
  name="${entry##*:}"
  pids=$(lsof -ti "tcp:$port" 2>/dev/null || true)
  if [[ -n "$pids" ]]; then
    echo "Killing $name (port $port): PIDs $pids"
    echo "$pids" | xargs kill -9 2>/dev/null || true
    killed=$((killed + 1))
  fi
done

if [[ "$killed" -eq 0 ]]; then
  echo "No processes found on any development ports."
else
  echo "Killed processes on $killed port(s)."
fi
