#!/usr/bin/env bash

gram_enrich_identity_payload() {
  local payload="$1"
  local email=""
  local commands="${GRAM_DEVICE_AGENT_COMMANDS:-device-agent,speakeasy-device-agent}"
  local timeout_tenths="${GRAM_DEVICE_AGENT_TIMEOUT_TENTHS:-15}"
  local old_ifs="$IFS"
  local command output tmp pid elapsed prefix trimmed

  IFS=,
  for command in $commands; do
    IFS="$old_ifs"
    command="${command#"${command%%[![:space:]]*}"}"
    command="${command%"${command##*[![:space:]]}"}"
    if [ -z "$command" ] || ! command -v "$command" >/dev/null 2>&1; then
      IFS=,
      continue
    fi

    tmp="$(mktemp "${TMPDIR:-/tmp}/gram-device-agent-identity.XXXXXX")" || {
      IFS=,
      continue
    }
    ("$command" identity >"$tmp" 2>/dev/null) &
    pid=$!
    elapsed=0
    while kill -0 "$pid" >/dev/null 2>&1 && [ "$elapsed" -lt "$timeout_tenths" ]; do
      sleep 0.1
      elapsed=$((elapsed + 1))
    done
    if kill -0 "$pid" >/dev/null 2>&1; then
      kill "$pid" >/dev/null 2>&1 || true
      wait "$pid" >/dev/null 2>&1 || true
      rm -f "$tmp"
      IFS=,
      continue
    fi
    wait "$pid" >/dev/null 2>&1 || true
    output=$(cat "$tmp" 2>/dev/null || true)
    rm -f "$tmp"

    if [[ "$output" =~ ^[[:space:]]*([A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})[[:space:]]*$ ]]; then
      email="${BASH_REMATCH[1]}"
    elif [[ "$output" =~ \"(email|user_email|userEmail|mail|preferred_username)\"[[:space:]]*:[[:space:]]*\"([A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})\" ]]; then
      email="${BASH_REMATCH[2]}"
    fi
    if [ -n "$email" ]; then
      break
    fi
    IFS=,
  done
  IFS="$old_ifs"

  if [ -z "$email" ]; then
    printf '%s' "$payload"
    return
  fi

  trimmed=$(printf '%s' "$payload" | sed 's/[[:space:]]*$//')
  case "$trimmed" in
    \{*\})
      if [[ "$trimmed" =~ \"user_email\"[[:space:]]*: ]]; then
        printf '%s' "$trimmed" | sed -E 's|"user_email"[[:space:]]*:[[:space:]]*"[^"]*"|"user_email":"'"$email"'"|g'
        return
      fi
      prefix="${trimmed%\}}"
      if [[ "$prefix" =~ ^\{[[:space:]]*$ ]]; then
        printf '{"user_email":"%s"}' "$email"
      else
        printf '%s,"user_email":"%s"}' "$prefix" "$email"
      fi
      ;;
    *)
      printf '%s' "$payload"
      ;;
  esac
}
