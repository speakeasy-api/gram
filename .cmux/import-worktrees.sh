#!/usr/bin/env bash
# Import existing git worktrees into cmux: one workspace per worktree, named
# after its branch. Idempotent — worktrees that already have a workspace with
# their cwd are skipped. cmux persists workspaces across restarts, so this is
# a one-time (or occasional) sync, not a startup requirement.
#
# Run from any terminal while cmux is running. Requirements:
#   - socket control mode must allow external clients:
#       defaults write com.cmuxterm.app socketControlMode automation
#     (default `cmuxOnly` rejects any process not started inside cmux)
#   - the CLI ships inside the app bundle, not on PATH; resolved below
#   - the socket lives under Application Support, not /tmp
set -euo pipefail

cmux() {
  local bin
  bin=$(type -P cmux || true)
  [ -n "$bin" ] || bin="/Applications/cmux.app/Contents/Resources/bin/cmux"
  CMUX_SOCKET_PATH="${CMUX_SOCKET_PATH:-$HOME/Library/Application Support/cmux/cmux.sock}" "$bin" "$@"
}

repo="${1:-$(git rev-parse --show-toplevel)}"

existing=$(cmux list-workspaces --json | python3 -c '
import json, sys
for ws in json.load(sys.stdin):
    print(ws.get("cwd") or "")
')

git -C "$repo" worktree list --porcelain | awk '/^worktree /{print $2}' |
while read -r wt; do
  if printf "%s\n" "$existing" | grep -Fxq "$wt"; then
    echo "skip (already open): $wt"
    continue
  fi
  branch=$(git -C "$wt" rev-parse --abbrev-ref HEAD)
  cmux new-workspace --cwd "$wt"
  id=$(cmux list-workspaces --json | python3 -c '
import json, sys
print(json.load(sys.stdin)[-1]["id"])
')
  cmux rename-workspace --workspace "$id" "$branch"
  echo "imported: $branch ($wt)"
done
