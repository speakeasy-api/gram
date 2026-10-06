#!/usr/bin/env bash

#MISE description="Inspect or apply a local account profile for the selected dev-idp user"
#MISE dir="{{ config_root }}"

#USAGE arg "[action]" help="status inspects; repair restores enterprise; apply selects a profile" {
#USAGE   choices "status" "repair" "apply"
#USAGE }
#USAGE arg "[profile]" help="Profile to use with apply" {
#USAGE   choices "enterprise" "payg" "active-trial" "expired-trial"
#USAGE }
#USAGE flag "--dry-run" help="Preview apply/repair without changing account state or stopping services"
#USAGE flag "--json" help="Write full result JSON to stdout; progress goes to stderr"

set -euo pipefail

if [[ $# -eq 0 ]]; then
  exec mise run account --help
fi

# No wake, seed, bootstrap, or identity switching. Dependencies must already be
# available; account-state validates the worktree and fails closed otherwise.
# Apply/repair stop this worktree's writers and restore their original running
# states, including on failure. Status and dry-run never manage services.
# Every invocation owns its output: concurrent builds cannot replace a binary
# between build and exec. The small supervisor forwards signals and waits for Go's
# bounded recovery before deleting the temporary binary (including error paths).
build_dir=$(mktemp -d "${TMPDIR:-/tmp}/gram-account.XXXXXXXX")
child=""
pending_signal=""
trap 'rm -rf -- "$build_dir"' EXIT
forward_signal() {
  pending_signal=$1
  if [[ -n "$child" ]]; then
    kill -s "$1" "$child" 2>/dev/null || true
  fi
}
trap 'forward_signal TERM' TERM
trap 'forward_signal INT' INT
run_child() {
  "$@" &
  child=$!
  if [[ -n "$pending_signal" ]]; then
    kill -s "$pending_signal" "$child" 2>/dev/null || true
  fi
  local status=0
  while true; do
    wait "$child" && status=0 || status=$?
    # wait can be interrupted by our trap; recovery still needs to finish.
    if ! kill -0 "$child" 2>/dev/null; then break; fi
  done
  child=""
  if [[ -n "$pending_signal" ]]; then
    case "$pending_signal" in INT) return 130 ;; TERM) return 143 ;; esac
  fi
  return "$status"
}
printf '%s\n' "Preparing command..." >&2
run_child mise run -q build:server-cache --out "$build_dir/gram-account-state"
run_child "$build_dir/gram-account-state" account-state "$@"
