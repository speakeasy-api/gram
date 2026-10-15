#!/usr/bin/env bash

# Claude Code WorktreeRemove hook. Removes the worktree through Worktrunk so the
# pre-remove hooks in .config/wt.toml tear down its MCP registration and stack.
# Falls back to plain `git worktree remove` when `wt` isn't installed.
#
# Claude Code confirms with the user before removing a worktree with changes,
# so removal is always forced here. Branch deletion follows Worktrunk's config
# (integrated branches are deleted, unmerged ones kept); the git fallback
# mirrors that with `git branch -d`.
#
# stdin: {"worktree_path": "<dir>", ...}

set -euo pipefail

path=$(jq -r .worktree_path)
[[ -d "$path" ]] || exit 0
root=$(git -C "$path" rev-parse --path-format=absolute --git-common-dir)
root=$(dirname "$root")

if command -v wt >/dev/null 2>&1; then
  (cd "$root" && wt remove --foreground --yes --format=json --force -- "$path" </dev/null) >&2
else
  branch=$(git -C "$path" branch --show-current)
  git -C "$root" worktree remove --force "$path" >&2
  # Like Worktrunk's default: delete the branch only if it is merged.
  [[ -n "$branch" ]] && git -C "$root" branch -d "$branch" >&2 2>/dev/null || true
fi
