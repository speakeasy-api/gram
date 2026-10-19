#!/usr/bin/env bash
# approval-watch: wait for a decision on one Speakeasy approval request.
# Prints one line per status change and exits once the request is decided.
#
# usage: approval-watch.sh <watch-url> <token-file> [interval-secs] [max-checks]
#
# The token is read from <token-file> and sent only in the Authorization
# header, through a private header file, so it never appears in a URL, the
# process list or shell history.
set -uo pipefail

url="${1:?usage: approval-watch.sh <watch-url> <token-file> [interval-secs] [max-checks]}"
token_file="${2:?usage: approval-watch.sh <watch-url> <token-file> [interval-secs] [max-checks]}"
interval="${3:-3}"
checks="${4:-1200}"

header_file="$(mktemp)"
trap 'rm -f "$header_file"' EXIT
chmod 600 "$header_file"
printf 'Authorization: Bearer %s\n' "$(tr -d '[:space:]' < "$token_file")" > "$header_file"

last=""
n=0
while [ "$n" -lt "$checks" ]; do
  n=$((n + 1))
  body="$(curl -fsS --max-time 10 -H @"$header_file" "$url" 2>/dev/null || true)"
  status="$(printf '%s' "$body" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([a-z_]*\)".*/\1/p')"
  if [ -z "$status" ]; then
    status="unreachable"
  fi
  if [ "$status" != "$last" ]; then
    echo "APPROVAL STATUS: $status"
    last="$status"
  fi
  case "$status" in
    approved)
      echo "APPROVED: retry the blocked tool call now with exactly the same arguments"
      exit 0
      ;;
    denied)
      echo "DENIED: do not retry; tell the user"
      exit 0
      ;;
    expired)
      echo "EXPIRED: nobody decided in time; tell the user"
      exit 0
      ;;
  esac
  sleep "$interval"
done
echo "WATCH TIMEOUT after $checks checks"
exit 1
