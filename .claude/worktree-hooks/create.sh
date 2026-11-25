#!/usr/bin/env bash

# Claude Code WorktreeCreate hook. Creates the worktree through Worktrunk so the
# project hooks in .config/wt.toml run, the same way the OpenCode
# speakeasy.worktrunk strategy does: workinit blocks, then the stack is
# provisioned in the background and left paused (`mise run wake` resumes it).
# Falls back to plain `git worktree add`, without any setup, when `wt` isn't
# installed.
#
# stdin: {"name": "<slug>", "cwd": "<dir>", ...}
# stdout: the worktree path, as the last line. Everything else goes to stderr.

set -euo pipefail

input=$(cat)
name=$(jq -r .name <<<"$input")
source_dir=$(jq -r .cwd <<<"$input")
root=$(git -C "$source_dir" rev-parse --path-format=absolute --git-common-dir)
root=$(dirname "$root")
dir="$root/.claude/worktrees/$name"
branch="claude/$name"

# Keep the root checkout on main. A dirty root is left alone, since `git switch`
# would carry non-conflicting edits over to main; worktree creation carries on
# with a warning either way.
if [[ "$(git -C "$root" branch --show-current)" != "main" ]]; then
  if [[ -n "$(git -C "$root" status --porcelain)" ]]; then
    echo "Warning: $root has uncommitted changes; not switching it to main" >&2
  else
    git -C "$root" switch main >&2 ||
      echo "Warning: could not switch $root to main" >&2
  fi
fi

if command -v wt >/dev/null 2>&1; then
  # A literal Jinja expression preserves paths containing template syntax.
  template="{{ $(jq -n --arg d "$dir" '$d') }}"
  out=$(cd "$source_dir" && wt \
    --config-set "worktree-path=$(jq -n --arg t "$template" '$t')" \
    switch --create --no-cd --yes --format=json --base=HEAD "$branch" </dev/null)
  path=$(jq -r .path <<<"$out")
  if [[ "$(realpath -m "$path")" != "$(realpath -m "$dir")" ]]; then
    echo "Worktrunk returned an unexpected worktree path: $path" >&2
    exit 1
  fi
else
  echo "wt not found; creating a plain git worktree without stack setup" >&2
  git -C "$source_dir" worktree add -b "$branch" "$dir" HEAD >&2
fi

echo "$dir"
