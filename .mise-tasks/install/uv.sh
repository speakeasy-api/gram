#!/usr/bin/env bash
#MISE description="Install Python dependencies"
#MISE hide=true
#MISE dir="{{ config_root }}"

set -euo pipefail

# pystreams pins the spaCy model (en-core-web-lg) to a GitHub release asset, so
# every sync that misses the uv cache fetches it straight from github.com.
# GitHub intermittently refuses those connections — HTTP/2 "refused stream", or
# a reset mid-transfer — and uv gives up after 3 attempts inside ~8s, which is
# too short to ride out a refusal that clears in seconds. The result is CI jobs
# failing for reasons unrelated to the change under test, including in the merge
# queue where this task runs for every PR.
#
# Retry the whole sync with exponential backoff, but only when the failure looks
# like a transport problem: a genuine failure (a stale lockfile under --locked,
# an unresolvable dependency) must still fail fast and loudly rather than
# burning the backoff budget first.
attempts="${UV_SYNC_ATTEMPTS:-4}"
delay="${UV_SYNC_RETRY_DELAY:-5}"

log="$(mktemp)"
trap 'rm -f "$log"' EXIT

for attempt in $(seq 1 "$attempts"); do
  # pipefail makes this reflect uv's exit status, not tee's.
  if uv sync "$@" 2>&1 | tee "$log"; then
    exit 0
  fi

  if ! grep -qiE 'failed to fetch|error sending request|stream error|request failed after|connection (reset|closed)|operation timed out' "$log"; then
    exit 1
  fi

  if [ "$attempt" -eq "$attempts" ]; then
    echo "uv sync: giving up after ${attempts} attempts" >&2
    exit 1
  fi

  echo "uv sync: transport failure on attempt ${attempt}/${attempts}, retrying in ${delay}s" >&2
  sleep "$delay"
  delay=$((delay * 2))
done
