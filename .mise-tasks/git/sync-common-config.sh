#!/usr/bin/env bash

#MISE description="Sync common config files from the main worktree"
#MISE hide=true
#MISE alias="gscc"

#USAGE flag "-f --force" help="Sync even if destinations have uncommitted changes"

set -e

main_worktree=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
current_worktree=$(git rev-parse --show-toplevel)

if [ "$main_worktree" = "$current_worktree" ]; then
  echo "Already in the main worktree, nothing to sync."
  exit 0
fi

sync_items=(
  .mise-tasks
  .claude
  .cursor
  .vscode
  local
  mise.local.toml
)

if [ "${usage_force:-false}" != "true" ]; then
  dirty=()
  for item in "${sync_items[@]}"; do
    if [ -n "$(git status --porcelain -- "$item" 2>/dev/null)" ]; then
      dirty+=("$item")
    fi
  done
  if [ ${#dirty[@]} -gt 0 ]; then
    echo "The following paths have uncommitted changes:"
    printf '  %s\n' "${dirty[@]}"
    echo "Aborting sync. Use --force to override."
    exit 1
  fi
fi

for item in "${sync_items[@]}"; do
  src="${main_worktree}/${item}"
  if [ -e "$src" ]; then
    echo "Syncing $item"
    rsync -a "$src" "$current_worktree/"
  fi
done

echo "Done."
