#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE hide="true"
#MISE alias="gwi"
#MISE description="Initialize a worktree"

#USAGE flag "--source <source>" help="Source worktree to copy from (defaults to main worktree)"

set -e

# Find the source worktree to copy shared files from
if [ -n "${usage_source:-}" ]; then
  main_worktree=$(cd "$usage_source" && pwd)
else
  main_worktree=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
fi
current_worktree=$(git rev-parse --show-toplevel)

if [ -z "$main_worktree" ] || [ "$main_worktree" = "$current_worktree" ]; then
  echo "Error: this task must be run from a git worktree, not the main working tree."
  exit 1
fi

copy_from_main=(
  ./mise.local.toml
  ./local
  ./.vscode
  ./.cursor
  ./.claude
)

for item in "${copy_from_main[@]}"; do
  src="${main_worktree}/${item}"
  [ -e "$src" ] && cp -r "$src" .
done

mise trust
mise run install:pnpm

suffix=$(head /dev/urandom | tr -dc 'a-z0-9' | head -c 4)
compose_project="gram-infra-${suffix}"
mise set --file mise.local.toml "COMPOSE_PROJECT_NAME=${compose_project}"

remap=$(mise run zero:remap-ports --format flat --file -)
for line in $remap; do
  key="${line%%=*}"
  # We need to first unset keys so that they are set in the correct order
  mise unset --file mise.local.toml "$key"
  mise set --file mise.local.toml "$line"
done

echo ✅ Updated all port mappings for new worktree