#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE hide="true"
#MISE description="Copy per-developer local config files from the main worktree"

#USAGE flag "--source <source>" help="Source worktree to copy from (defaults to main worktree)"
#USAGE flag "--force" help="Overwrite files that already exist here instead of only filling in missing ones"

set -e

# Gitignored files a developer configures once and expects in every worktree.
# Deliberately NOT mise.local.toml: that one is worktree-specific (ports,
# Compose project, Temporal namespace), so git:workinit copies it once and
# git:worksync preserves it. Anything listed here must be safe to hold an
# identical copy of in every worktree at the same time.
local_config_from_main=(
  ./client/dashboard/vite.config.local.ts
  ./client/dashboard/src/dev-slot.local.tsx
)

if [ -n "${usage_source:-}" ]; then
  main_worktree=$(cd "$usage_source" && pwd)
else
  main_worktree=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
fi
current_worktree=$(git rev-parse --show-toplevel)

# Running this in the main worktree would copy every file onto itself.
if [ -z "$main_worktree" ] || [ "$main_worktree" = "$current_worktree" ]; then
  exit 0
fi

copied=0
for item in "${local_config_from_main[@]}"; do
  src="${main_worktree}/${item}"
  [ -e "$src" ] || continue
  # Default to filling in only what is missing: this worktree's copy may have
  # been edited for the work in progress here, and silently reverting it to
  # main's would lose that.
  if [ "${usage_force:-false}" != "true" ] && [ -e "$item" ]; then
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
  echo "✅ Copied ${copied} local config file(s) from the main worktree."
fi
