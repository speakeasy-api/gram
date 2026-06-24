#!/usr/bin/env bash
# Shared local authentication helper for Gram hook senders.

gram_hooks_auth_file() {
  if [ -n "${GRAM_HOOKS_AUTH_FILE:-}" ]; then
    printf '%s' "$GRAM_HOOKS_AUTH_FILE"
    return 0
  fi
  local config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
  printf '%s/gram/hooks-auth.env' "$config_home"
}

gram_hooks_auth_value() {
  local path="$1"
  local key="$2"
  sed -n "s/^${key}=//p" "$path" 2>/dev/null | sed -n '1p'
}

gram_hooks_read_auth() {
  local server_url="$1"
  local path
  path="$(gram_hooks_auth_file)"
  if [ ! -r "$path" ]; then
    return 1
  fi
  GRAM_HOOKS_CACHED_SERVER_URL="$(gram_hooks_auth_value "$path" "server_url")"
  GRAM_HOOKS_CACHED_API_KEY="$(gram_hooks_auth_value "$path" "api_key")"
  GRAM_HOOKS_CACHED_PROJECT="$(gram_hooks_auth_value "$path" "project")"
  GRAM_HOOKS_CACHED_EMAIL="$(gram_hooks_auth_value "$path" "email")"
  [ "$GRAM_HOOKS_CACHED_SERVER_URL" = "$server_url" ] || return 1
  [ -n "$GRAM_HOOKS_CACHED_API_KEY" ] || return 1
}

gram_hooks_write_auth() {
  local server_url="$1"
  local api_key="$2"
  local project="$3"
  local email="${4:-}"
  local path
  path="$(gram_hooks_auth_file)"
  mkdir -p "$(dirname "$path")" || return 1
  chmod 700 "$(dirname "$path")" 2>/dev/null || true
  local tmp="${path}.tmp.$$"
  local old_umask
  old_umask="$(umask)"
  umask 077
  {
    printf 'server_url=%s\n' "$server_url"
    printf 'api_key=%s\n' "$api_key"
    printf 'project=%s\n' "$project"
    printf 'email=%s\n' "$email"
  } >"$tmp" || {
    rm -f "$tmp"
    umask "$old_umask"
    return 1
  }
  umask "$old_umask"
  mv "$tmp" "$path"
}

gram_hooks_forget_auth() {
  local path
  path="$(gram_hooks_auth_file)"
  rm -f "$path"
}

gram_hooks_login() {
  local server_url="$1"
  local project_hint="$2"
  if [ "${GRAM_HOOKS_DISABLE_LOCAL_AUTH:-}" = "1" ]; then
    return 1
  fi
  echo "Speakeasy hooks need a Gram hooks API key before this session can start." >&2
  echo "Set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG, or cache a key by sourcing hooks/auth.sh and running:" >&2
  echo "  gram_hooks_write_auth '$server_url' '<hooks-api-key>' '${project_hint}' '<email>'" >&2
  return 1
}

gram_hooks_write_curl_config() {
  local api_key="$1"
  local project="$2"
  auth_config=$(mktemp "${TMPDIR:-/tmp}/gram-hooks-curl.XXXXXX") || return 1
  chmod 600 "$auth_config" || true
  printf 'header = "Gram-Key: %s"\n' "$api_key" >"$auth_config"
  printf 'header = "Gram-Project: %s"\n' "$project" >>"$auth_config"
  auth_config_arg=(--config "$auth_config")
}

gram_hooks_cleanup_auth_config() {
  if [ -n "${auth_config:-}" ]; then
    rm -f "$auth_config"
  fi
}
trap gram_hooks_cleanup_auth_config EXIT

gram_hooks_prepare_auth() {
  local server_url="$1"
  local project_hint="$2"
  local failure_exit="$3"
  local force="${4:-}"
  local api_key project email

  api_key=""
  project=""
  email=""
  if [ "$force" != "force" ]; then
    api_key="${GRAM_HOOKS_API_KEY:-${GRAM_API_KEY:-}}"
    project="${GRAM_HOOKS_PROJECT_SLUG:-${GRAM_PROJECT_SLUG:-}}"
  fi

  if [ -z "$api_key" ]; then
    if [ "$force" != "force" ]; then
      gram_hooks_read_auth "$server_url" 2>/dev/null || true
    else
      GRAM_HOOKS_CACHED_API_KEY=""
      GRAM_HOOKS_CACHED_PROJECT=""
      GRAM_HOOKS_CACHED_EMAIL=""
    fi
    if [ -z "${GRAM_HOOKS_CACHED_API_KEY:-}" ]; then
      if ! gram_hooks_login "$server_url" "$project_hint"; then
        echo "Speakeasy hooks could not authenticate with Gram." >&2
        exit "$failure_exit"
      fi
      gram_hooks_read_auth "$server_url" 2>/dev/null || true
    fi
    api_key="${GRAM_HOOKS_CACHED_API_KEY:-}"
    project="${GRAM_HOOKS_CACHED_PROJECT:-}"
    email="${GRAM_HOOKS_CACHED_EMAIL:-}"
  fi

  if [ -z "$project" ]; then
    project="$project_hint"
  fi
  if [ -z "$api_key" ] || [ -z "$project" ]; then
    echo "Speakeasy hooks are missing Gram authentication or project selection." >&2
    exit "$failure_exit"
  fi

  if ! gram_hooks_write_curl_config "$api_key" "$project"; then
    echo "Speakeasy hooks could not prepare Gram authentication." >&2
    exit "$failure_exit"
  fi

  if [ -n "$email" ]; then
    export GRAM_HOOKS_AUTH_EMAIL="$email"
  fi
}

gram_hooks_post_authenticated() {
  local server_url="$1"
  local payload="$2"
  local max_time="$3"
  local project_hint="$4"
  local failure_exit="$5"
  shift 5

  gram_hooks_prepare_auth "$server_url" "$project_hint" "$failure_exit"
  gram_http_post "${server_url}/rpc/hooks.ingest" "$payload" "$max_time" \
    "$@" \
    ${auth_config_arg[@]+"${auth_config_arg[@]}"}
  local first_status="$GRAM_HTTP_CODE"
  if { [ "$first_status" = "401" ] || [ "$first_status" = "403" ]; } && [ "${GRAM_HOOKS_DISABLE_LOCAL_AUTH:-}" != "1" ]; then
    gram_hooks_forget_auth
    gram_hooks_prepare_auth "$server_url" "$project_hint" "$failure_exit" force
    gram_http_post "${server_url}/rpc/hooks.ingest" "$payload" "$max_time" \
      "$@" \
      ${auth_config_arg[@]+"${auth_config_arg[@]}"}
  fi
}
