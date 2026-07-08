# Shared retryable HTTP helper for Gram hook senders.
#
# Sourced (not executed) by every plugin's send/hook scripts so all plugins
# share one transport with identical retry and idempotency behavior.
#
# Why retries are safe: the server de-duplicates on the per-invocation
# Idempotency-Key header (see gram_http_post), so re-sending the same request
# after a transient reset stores the event exactly once.
#
# Usage:
#   . "$script_dir/http.sh"
#   gram_http_post "$url" "$payload" max_time [extra curl args...]
#   # then read $GRAM_HTTP_CODE (e.g. "200", "000") and $GRAM_HTTP_BODY.
#
# gram_http_post returns 0 once curl produced a definitive HTTP status (any
# code, including 4xx/5xx — the caller decides allow/block from $GRAM_HTTP_CODE)
# and non-zero only when every attempt failed to reach the server.

GRAM_HTTP_MAX_ATTEMPTS="${GRAM_HTTP_MAX_ATTEMPTS:-4}"
GRAM_HTTP_BACKOFF_BASE="${GRAM_HTTP_BACKOFF_BASE:-1}"

# gram_new_idempotency_token emits one token per shell invocation, captured
# once and reused across retries so every attempt of the same logical delivery
# carries the same token.
gram_new_idempotency_token() {
  if [ -n "${GRAM_IDEMPOTENCY_TOKEN:-}" ]; then
    printf '%s' "$GRAM_IDEMPOTENCY_TOKEN"
    return 0
  fi
  if command -v uuidgen >/dev/null 2>&1; then
    GRAM_IDEMPOTENCY_TOKEN=$(uuidgen | tr '[:upper:]' '[:lower:]')
  elif [ -r /proc/sys/kernel/random/uuid ]; then
    GRAM_IDEMPOTENCY_TOKEN=$(cat /proc/sys/kernel/random/uuid)
  else
    GRAM_IDEMPOTENCY_TOKEN="$(date +%s)-$$-${RANDOM:-0}${RANDOM:-0}"
  fi
  printf '%s' "$GRAM_IDEMPOTENCY_TOKEN"
}

# _gram_http_is_transient returns 0 for a transient failure worth retrying: a
# connection-level error (no response) or a 5xx. A clean 2xx/3xx/4xx is a
# definitive answer and must NOT be retried.
_gram_http_is_transient() {
  local curl_exit="$1"
  local http_code="$2"
  case "$curl_exit" in
    # 6 DNS, 7 connect, 28 timeout, 35 TLS handshake, 52 empty reply,
    # 55 send error, 56 recv error (the connection-reset class).
    6 | 7 | 28 | 35 | 52 | 55 | 56) return 0 ;;
  esac
  if [ "$curl_exit" -ne 0 ] && { [ -z "$http_code" ] || [ "$http_code" = "000" ]; }; then
    return 0
  fi
  if [ "$http_code" -ge 500 ] 2>/dev/null; then
    return 0
  fi
  return 1
}

# gram_http_post POSTs $2 to $1 with a per-attempt timeout of $3 seconds,
# retrying transient failures with backoff. Remaining args pass verbatim to
# curl. It always adds Content-Type and the reused Idempotency-Key header.
gram_http_post() {
  local _url="$1"
  local _payload="$2"
  local _max_time="$3"
  shift 3

  local _token
  _token=$(gram_new_idempotency_token)

  local attempt=1
  local response curl_exit
  GRAM_HTTP_CODE="000"
  GRAM_HTTP_BODY=""
  while [ "$attempt" -le "$GRAM_HTTP_MAX_ATTEMPTS" ]; do
    response=$(printf '%s' "$_payload" | curl -s -w "\n%{http_code}" -X POST \
      -H "Content-Type: application/json" \
      -H "Idempotency-Key: ${_token}" \
      "$@" \
      --data-binary @- \
      --max-time "$_max_time" \
      "$_url")
    curl_exit=$?

    GRAM_HTTP_CODE=$(printf '%s' "$response" | tail -1)
    GRAM_HTTP_BODY=$(printf '%s' "$response" | sed '$d')

    if ! _gram_http_is_transient "$curl_exit" "$GRAM_HTTP_CODE"; then
      # Definitive: a 2xx/3xx/4xx (success) or a non-transient curl error
      # (bad usage/URL — retrying won't help). Distinguish via the code.
      if [ "$curl_exit" -eq 0 ]; then
        return 0
      fi
      return 1
    fi

    if [ "$attempt" -lt "$GRAM_HTTP_MAX_ATTEMPTS" ]; then
      sleep "$((GRAM_HTTP_BACKOFF_BASE * attempt))"
    fi
    attempt=$((attempt + 1))
  done

  # Exhausted retries. A real HTTP status (e.g. a persistent 5xx) → report
  # success so the caller can act on the code; otherwise the server was
  # unreachable.
  if [ "$GRAM_HTTP_CODE" != "000" ] && [ -n "$GRAM_HTTP_CODE" ]; then
    return 0
  fi
  return 1
}
