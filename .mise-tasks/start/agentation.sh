#!/usr/bin/env bash

#MISE description="Set up and start Agentation for this worktree (opt-in)"
#MISE dir="{{ config_root }}"

set -euo pipefail

if [ ! -x client/dashboard/node_modules/.bin/agentation-mcp ]; then
  aube install --frozen-lockfile
fi

# Reuse worktree allocation, but only save Agentation's missing configuration.
# Existing assignments are preserved, including ports reserved by other worktrees.
remap=$(mise run zero:remap-ports --preserve --format flat --file -)
while IFS= read -r line; do
  case "$line" in
    GRAM_AGENTATION_PORT=*|VITE_AGENTATION_URL=*)
      key="${line%%=*}"
      mise unset --file mise.local.toml "$key"
      mise set --file mise.local.toml "$line"
      ;;
  esac
done <<< "$remap"
mise set --file mise.local.toml VITE_AGENTATION_ENABLED=true

# Pitchfork owns the process, so this task returns once the daemon is ready.
pitchfork supervisor start
mise exec -- pitchfork start agentation

# Reload the overlay configuration without waking a stopped dashboard/stack.
dashboard_running=$(pitchfork list --project --status running --json | node -e '
  let input = "";
  process.stdin.on("data", chunk => input += chunk);
  process.stdin.on("end", () => console.log(JSON.parse(input).some(daemon => daemon.name === "dashboard")));
')
if [ "$dashboard_running" = true ]; then
  mise exec -- pitchfork restart dashboard
fi

echo 'Agentation is running in the background. Open or refresh the dashboard to use the annotation toolbar.'
