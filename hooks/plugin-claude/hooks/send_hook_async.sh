#!/usr/bin/env bash

# Wrapper that keeps an event off the critical path when hooks.json async=true
# is unreliable (Cowork drops async Stop-class hooks). The event is registered
# synchronously so the client dispatches it; this script copies stdin before the
# parent hook exits, then forwards the payload in a background process. Used for
# Stop/SubagentStop, which carry no deny decision.

set -u

tmp="$(mktemp "${TMPDIR:-/tmp}/gram-hook.XXXXXX")" || exit 0
if ! cat > "$tmp"; then
  rm -f "$tmp"
  exit 0
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

(
  bash "$script_dir/send_hook.sh" < "$tmp" >/dev/null 2>&1
  rm -f "$tmp"
) >/dev/null 2>&1 &

exit 0
