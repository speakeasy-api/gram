#!/usr/bin/env bash

# Claude Code hook that pauses this worktree's stack once a session has nothing
# left to do: the Claude Code counterpart of the OpenCode speakeasy.pause-stack
# plugin (.opencode/plugins/pause-stack.js). It is opt-in the same way. Add
# `speakeasy.pause-stack` to the colon-separated GRAM_OPENCODE_PLUGINS in
# mise.local.toml (see .claude/README.md). Without it, every event is a no-op.
#
# Wired up in .claude/settings.json for these events. The hooks are synchronous,
# so one session's events never overlap: a Stop cannot clear the marker of the
# prompt that follows it, and SessionEnd always comes last.
# - UserPromptSubmit marks the session busy in the worktree's git dir.
# - Stop fires at the end of every turn, including turns that start background
#   subagents, shells or monitors. Those wake the session again when they
#   finish, so a Stop that still lists any of them in background_tasks only
#   keeps the session busy. The Stop after the last of them settles the
#   session. If no other session in the worktree is busy, the stack is paused.
#   Scheduled wakeups (session_crons) are not waited for, because they can be
#   hours away and waking the stack again is cheap.
# - SessionEnd forgets the session and settles it like Stop, so closing the last
#   session pauses the stack even when its final turn was interrupted.
#
# An interrupted turn fires no Stop, so the session stays busy until its next
# turn ends or it closes. Until then the idle-pause sweep still applies.
#
# stdin: {"hook_event_name": "...", "session_id": "...", "cwd": "...", "background_tasks": [...]}

set -euo pipefail

input=$(cat)
event=$(jq -r .hook_event_name <<<"$input")
session=$(jq -r .session_id <<<"$input")
cwd=$(jq -r .cwd <<<"$input")

[[ "$session" =~ ^[A-Za-z0-9_-]+$ ]] || exit 0
root=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null) || exit 0
gitdir=$(git -C "$root" rev-parse --absolute-git-dir)
sessions="$gitdir/gram-claude-sessions"
marker="$sessions/$session"

# A closed session never holds the stack up, whether or not the hook is enabled.
[[ "$event" == SessionEnd ]] && rm -f "$marker"

# A worktree that was never provisioned has no mise.local.toml of its own, and
# one nested in the main checkout then resolves the main checkout's stack.
[[ -f "$root/mise.local.toml" ]] || exit 0

# Like .opencode/lib/plugins.js: an unset variable or failed lookup leaves the
# hook disabled.
(cd "$root" && mise env --json) 2>/dev/null |
  jq -e --arg id speakeasy.pause-stack \
    '.GRAM_OPENCODE_PLUGINS | strings | split(":") | any(.[]; gsub("^\\s+|\\s+$"; "") == $id)' \
    >/dev/null || exit 0

busy() {
  mkdir -p "$sessions"
  : >"$marker"
}

case "$event" in
UserPromptSubmit)
  busy
  exit 0
  ;;
Stop)
  # The array is missing when Claude Code cannot reach its task registry, and
  # a failed lookup leaves the stack alone. In-flight work keeps the session
  # busy, including turns that started without a prompt.
  if ! jq -e '.background_tasks | arrays | length == 0' <<<"$input" >/dev/null; then
    busy
    exit 0
  fi
  rm -f "$marker"
  ;;
SessionEnd) ;;
*) exit 0 ;;
esac

# Another session in this worktree is still busy. The last one to settle pauses.
compgen -G "$sessions/*" >/dev/null && exit 0

# Already paused, or the initial boot is still running. Like idle-pause, stale
# boot markers and reused PIDs do not count.
[[ -e "$gitdir/gram-stack-paused" ]] && exit 0
pid=$(cat "$gitdir/gram-stack-boot.pid" 2>/dev/null || true)
pid=${pid//[[:space:]]/}
if [[ "$pid" =~ ^[1-9][0-9]*$ ]] && ps -o command= -p "$pid" 2>/dev/null | grep -q workboot; then
  exit 0
fi

# Like the idle sweep, hand the pause to pitchfork so it finishes even if the
# session closes partway through. Without a supervisor, pause in the background:
# the hook is synchronous, and SessionEnd hooks get about a second and a half.
cd "$root"
if ! pitchfork start pause-stack >&2; then
  nohup mise run pause </dev/null >/dev/null 2>&1 &
fi
