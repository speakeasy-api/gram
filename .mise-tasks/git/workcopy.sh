#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE hide=true
#MISE description="Copy per-developer config between this worktree and the main one. Use --mode promote to send your local config back to main so future worktrees inherit it."

#USAGE flag "--source <source>" help="Main worktree to copy to/from (defaults to the real one)"
#USAGE flag "--force" help="Overwrite files that already exist at the destination"
#USAGE flag "--mode <mode>" help="What to copy, and which way" {
#USAGE   choices "init" "sync" "promote"
#USAGE }

set -e

# Per-developer config a developer writes once and expects in every worktree.
# This is the only list that travels in both directions, so anything added here
# must be safe to hold an identical copy of in every worktree at once — no
# ports, no per-worktree identity.
local_config=(
  ./client/dashboard/vite.config.local.ts
  ./client/dashboard/src/dev-slot.local.tsx
)

# Worktree scaffolding, copied from main only when a worktree is created.
# mise.local.toml belongs here rather than above because it is worktree-specific
# — ports, Compose project, Temporal namespace — so it must never be refreshed
# from main, nor promoted back to it. .mise-tasks is last because copying it
# overwrites this very script; keeping it at the end leaves nothing but the
# summary still to run.
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
# here worth keeping. sync and promote only fill in what is missing at the
# destination, because a copy that is already there may have been edited for the
# work in progress beside it, and silently replacing it would lose that. Pass
# --force when replacing it is what you actually mean.
case "${usage_mode:-sync}" in
  init)
    items=("${local_config[@]}" "${scaffolding_from_main[@]}")
    overwrite=true
    to_main=false
    ;;
  sync)
    items=("${local_config[@]}")
    overwrite="${usage_force:-false}"
    to_main=false
    ;;
  promote)
    items=("${local_config[@]}")
    overwrite="${usage_force:-false}"
    to_main=true
    ;;
  *)
    echo "Error: --mode must be 'init', 'sync' or 'promote'."
    exit 1
    ;;
esac

copied=0
skipped=0
for item in "${items[@]}"; do
  if [ "$to_main" = "true" ]; then
    src="${item}"
    dest="${main_worktree}/${item}"
  else
    src="${main_worktree}/${item}"
    dest="${item}"
  fi

  [ -e "$src" ] || continue
  if [ "$overwrite" != "true" ] && [ -e "$dest" ]; then
    skipped=$((skipped + 1))
    continue
  fi

  if [ -d "$src" ]; then
    tools/rclone copy --metadata --links --create-empty-src-dirs "$src" "$dest"
  else
    tools/rclone copyto --metadata --links "$src" "$dest"
  fi
  echo "  + ${item}"
  copied=$((copied + 1))
done

if [ "$to_main" = "true" ]; then
  where="to the main worktree"
else
  where="from the main worktree"
fi

if [ "$copied" -gt 0 ]; then
  echo "✅ Copied ${copied} item(s) ${where}."
fi
if [ "$skipped" -gt 0 ]; then
  echo "ℹ️  Left ${skipped} existing item(s) alone; pass --force to replace them."
fi
