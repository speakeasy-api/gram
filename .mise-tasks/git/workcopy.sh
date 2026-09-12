#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE hide="true"
#MISE description="Copy files from the main worktree into this one"

#USAGE flag "--source <source>" help="Source worktree to copy from (defaults to main worktree)"
#USAGE flag "--scope <scope>" help="Which set of files to copy" {
#USAGE   choices "init" "sync"
#USAGE }

set -e

# Per-developer config a developer writes once and expects in every worktree.
# Copied when a worktree is created and topped up by git:worksync, so anything
# listed here must be safe to hold an identical copy of in every worktree at
# once — no ports, no per-worktree identity.
local_config_from_main=(
  ./client/dashboard/vite.config.local.ts
  ./client/dashboard/src/dev-slot.local.tsx
)

# Worktree scaffolding, copied only when the worktree is created. mise.local.toml
# belongs here rather than above because it is worktree-specific — ports, Compose
# project, Temporal namespace — so git:worksync must preserve it, never refresh
# it from main. .mise-tasks is last because copying it overwrites this very
# script; keeping it at the end leaves nothing but the summary still to run.
scaffolding_from_main=(
  ./local
  ./.vscode
  ./.cursor
  ./.claude
  ./mise.local.toml
  ./.mise-tasks
)

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

# init takes main's copies outright: the worktree is new, so there is nothing
# here worth keeping. sync only fills in what is missing — an existing worktree's
# local config may have been edited for the work in progress in it, and silently
# reverting that to main's copy would lose it.
case "${usage_scope:-sync}" in
  init)
    items=("${local_config_from_main[@]}" "${scaffolding_from_main[@]}")
    overwrite=true
    ;;
  sync)
    items=("${local_config_from_main[@]}")
    overwrite=false
    ;;
  *)
    echo "Error: --scope must be 'init' or 'sync'."
    exit 1
    ;;
esac

copied=0
for item in "${items[@]}"; do
  src="${main_worktree}/${item}"
  [ -e "$src" ] || continue
  if [ "$overwrite" != "true" ] && [ -e "$item" ]; then
    continue
  fi
  if [ -d "$src" ]; then
    tools/rclone copy --metadata --links --create-empty-src-dirs "$src" "$item"
  else
    tools/rclone copyto --metadata --links "$src" "$item"
  fi
  echo "  + ${item}"
  copied=$((copied + 1))
done

if [ "$copied" -gt 0 ]; then
  echo "✅ Copied ${copied} item(s) from the main worktree."
fi
