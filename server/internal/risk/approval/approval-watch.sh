#!/usr/bin/env bash
# approval-watch: wait for a decision on one Speakeasy approval request.
# Prints one line per status change and exits once the request is decided.
#
# usage: approval-watch.sh <watch-url> <token-file> [interval-secs] [max-checks]
#
# The token is read from <token-file> and sent only in the Authorization
# header, through a private header file, so it never appears in a URL, the
# process list or shell history. It is sent only over HTTPS, except to a
# local development server.
set -uo pipefail

usage="usage: approval-watch.sh <watch-url> <token-file> [interval-secs] [max-checks]"
url="${1:?$usage}"
token_file="${2:?$usage}"
interval="${3:-3}"
# 24 hours at the default interval: the longest window a request can have.
checks="${4:-28800}"

# The host is everything between the scheme and the first slash. Credentials
# in a URL (user@host) are refused outright: they can make an allowed-looking
# prefix send the token to another host.
scheme="${url%%://*}"
rest="${url#*://}"
authority="${rest%%/*}"
host="${authority%:*}"
case "$authority" in
  *@*)
    echo "WATCH FAILED: refusing a watch URL with credentials in it; tell the user"
    exit 2
    ;;
esac
case "$scheme" in
  https) ;;
  http)
    if [ "$host" != "localhost" ] && [ "$host" != "127.0.0.1" ]; then
      echo "WATCH FAILED: refusing to send the token to a non-HTTPS URL; tell the user"
      exit 2
    fi
    ;;
  *)
    echo "WATCH FAILED: refusing to send the token to a non-HTTPS URL; tell the user"
    exit 2
    ;;
esac

token="$(tr -d '[:space:]' < "$token_file" 2>/dev/null || true)"
if [ -z "$token" ]; then
  echo "WATCH FAILED: cannot read a token from $token_file; save the request token there first"
  exit 2
fi

header_file="$(mktemp)"
trap 'rm -f "$header_file"' EXIT
chmod 600 "$header_file"
printf 'Authorization: Bearer %s\n' "$token" > "$header_file"
unset token

last=""
n=0
while [ "$n" -lt "$checks" ]; do
  n=$((n + 1))
  response="$(curl -sS --max-time 10 --proto '=https,http' -H @"$header_file" -w '\n%{http_code}' "$url" 2>/dev/null || true)"
  code="$(printf '%s' "$response" | tail -n 1)"
  body="$(printf '%s' "$response" | sed '$d')"
  case "$code" in
    401 | 403 | 404)
      echo "WATCH FAILED: the server rejected the request token (HTTP $code); tell the user"
      exit 1
      ;;
  esac
  # Only a 200 carries a status to trust; anything else is a transient
  # failure, so keep waiting rather than read an error body as a decision.
  status=""
  if [ "$code" = "200" ]; then
    status="$(printf '%s' "$body" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([a-z_]*\)".*/\1/p')"
  fi
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
