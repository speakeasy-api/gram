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

gram_hooks_manual_auth_instructions() {
  local server_url="$1"
  local project_hint="$2"
  echo "Speakeasy hooks need a Gram hooks API key before events can be recorded." >&2
  echo "Set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG, or cache a key by sourcing hooks/auth.sh and running:" >&2
  echo "  gram_hooks_write_auth '$server_url' '<hooks-api-key>' '${project_hint}' '<email>'" >&2
}

# gram_hooks_urldecode decodes URL-encoded values (+ as space, %XX escapes).
# Literal backslashes are routed through %5C so printf %b cannot interpret
# them as escape sequences.
gram_hooks_urldecode() {
  local data="${1//+/ }"
  data="${data//\\/%5C}"
  printf '%b' "${data//%/\\x}"
}

# gram_hooks_nc_listen_styles orders candidate nc invocation styles by a
# usage-text sniff: host_port = BSD/OpenBSD (nc -l 127.0.0.1 PORT), dash_p_local
# and dash_p = GNU/busybox (-p PORT, loopback-bound when -s is accepted). The
# sniff only ranks; each style is verified with a live HTTP self-probe before
# the browser opens.
gram_hooks_nc_listen_styles() {
  local help_text
  help_text="$(nc -h 2>&1 || true)"
  case "$help_text" in
    *--local-port* | *"-p PORT"*) printf 'dash_p_local dash_p host_port' ;;
    *) printf 'host_port dash_p_local dash_p' ;;
  esac
}

gram_hooks_nc_listen() {
  case "$1" in
    dash_p_local) nc -l -p "$2" -s 127.0.0.1 2>/dev/null ;;
    dash_p) nc -l -p "$2" 2>/dev/null ;;
    *) nc -l 127.0.0.1 "$2" 2>/dev/null ;;
  esac
}

gram_hooks_login_http_response() {
  local status="$1"
  local body="$2"
  local reason="OK"
  if [ "$status" = "204" ]; then
    reason="No Content"
  fi
  printf 'HTTP/1.1 %s %s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: %s\r\nConnection: close\r\n\r\n%s' \
    "$status" "$reason" "${#body}" "$body"
}

gram_hooks_login_success_html() {
  printf '<!doctype html><html><head><title>Speakeasy hooks connected</title></head><body style="font-family:sans-serif;text-align:center;padding-top:4rem"><h1>Authentication successful</h1><p>Speakeasy hooks are connected. You can close this tab.</p></body></html>'
}

# gram_hooks_login_handle_request reads one HTTP request from stdin (the nc
# pipe), captures the /callback query string into a file, and writes the
# response to stdout (piped back to the client through the fifo). Requests
# without api_key (favicon, probes) get a 204 so the serve loop keeps waiting
# for the dashboard's real redirect.
gram_hooks_login_handle_request() {
  local dir="$1"
  local request_line="" line="" path_query=""
  IFS= read -r -t 10 request_line || request_line=""
  request_line="${request_line%$'\r'}"
  if [ -z "$request_line" ]; then
    return 0
  fi
  while IFS= read -r -t 10 line; do
    line="${line%$'\r'}"
    if [ -z "$line" ]; then
      break
    fi
  done
  path_query="${request_line#* }"
  path_query="${path_query%% *}"
  case "$path_query" in
    /callback\?*api_key=*)
      printf '%s' "${path_query#*\?}" >"$dir/query.tmp"
      mv "$dir/query.tmp" "$dir/query"
      gram_hooks_login_http_response 200 "$(gram_hooks_login_success_html)"
      ;;
    *)
      gram_hooks_login_http_response 204 ""
      ;;
  esac
}

# gram_hooks_login_serve accepts connections one at a time until the callback
# query is captured, a stop file appears, or the request budget runs out.
# fd 4 holds the fifo open read-write so neither nc nor the handler can
# deadlock opening it, and a failed nc bind degrades to a fast, bounded loop
# that the probe below detects instead of a hung orphan process.
gram_hooks_login_serve() {
  local style="$1"
  local dir="$2"
  local port="$3"
  local requests=0
  exec 4<>"$dir/fifo"
  while [ "$requests" -lt 32 ] && [ ! -e "$dir/stop" ] && [ ! -s "$dir/query" ]; do
    gram_hooks_nc_listen "$style" "$port" <"$dir/fifo" | gram_hooks_login_handle_request "$dir" >"$dir/fifo"
    requests=$((requests + 1))
  done
  exec 4>&-
}

gram_hooks_login_probe() {
  local port="$1"
  local i=0
  while [ "$i" -lt 3 ]; do
    i=$((i + 1))
    if curl -s -o /dev/null --max-time 2 "http://127.0.0.1:${port}/gram-probe" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# gram_hooks_login_stop_server unblocks a listening nc with a loopback poke so
# the serve loop can observe the stop file, then reaps the background job.
gram_hooks_login_stop_server() {
  local port="$1"
  local pid="$2"
  local dir="$3"
  : >"$dir/stop" 2>/dev/null || true
  curl -s -o /dev/null --max-time 1 "http://127.0.0.1:${port}/gram-stop" 2>/dev/null || true
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
}

gram_hooks_open_browser() {
  local url="$1"
  case "$(uname -s 2>/dev/null)" in
    Darwin)
      if command -v open >/dev/null 2>&1; then
        open "$url" 2>/dev/null && return 0
      fi
      ;;
    *)
      if command -v xdg-open >/dev/null 2>&1; then
        xdg-open "$url" >/dev/null 2>&1 && return 0
      fi
      ;;
  esac
  return 1
}

gram_hooks_cleanup_login() {
  if [ -n "${GRAM_HOOKS_LOGIN_TMPDIR:-}" ]; then
    : >"$GRAM_HOOKS_LOGIN_TMPDIR/stop" 2>/dev/null || true
  fi
  if [ -n "${GRAM_HOOKS_LOGIN_PORT:-}" ]; then
    curl -s -o /dev/null --max-time 1 "http://127.0.0.1:${GRAM_HOOKS_LOGIN_PORT}/gram-stop" 2>/dev/null || true
  fi
  if [ -n "${GRAM_HOOKS_LOGIN_SERVER_PID:-}" ]; then
    kill "$GRAM_HOOKS_LOGIN_SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "${GRAM_HOOKS_LOGIN_TMPDIR:-}" ]; then
    rm -rf "$GRAM_HOOKS_LOGIN_TMPDIR"
  fi
}

# gram_hooks_login mints a hooks-scoped API key via the dashboard browser flow:
# start a one-shot localhost listener, open the dashboard with cli_callback_url
# pointing at it, wait for the api_key redirect, and cache the result with
# gram_hooks_write_auth. Only interactive entry points run this —
# auth_preflight.sh and login.sh export GRAM_HOOKS_INTERACTIVE=1; per-event
# hook senders never block on a browser.
gram_hooks_login() {
  local server_url="$1"
  local project_hint="$2"

  if [ "${GRAM_HOOKS_DISABLE_LOCAL_AUTH:-}" = "1" ]; then
    return 1
  fi
  if [ "${GRAM_HOOKS_INTERACTIVE:-}" != "1" ]; then
    gram_hooks_manual_auth_instructions "$server_url" "$project_hint"
    return 1
  fi
  if [ -n "${CI:-}" ] || [ -n "${SSH_CONNECTION:-}" ] || [ -n "${SSH_TTY:-}" ]; then
    echo "Speakeasy hooks: no local browser available for login. Set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG instead." >&2
    return 1
  fi
  case "$(uname -s 2>/dev/null)" in
    Darwin) ;;
    *)
      if [ -z "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
        echo "Speakeasy hooks: no graphical session detected; skipping browser login. Set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG instead." >&2
        return 1
      fi
      ;;
  esac
  local dep
  for dep in nc mkfifo curl date; do
    if ! command -v "$dep" >/dev/null 2>&1; then
      echo "Speakeasy hooks: this machine is missing '$dep' for browser login." >&2
      gram_hooks_manual_auth_instructions "$server_url" "$project_hint"
      return 1
    fi
  done

  # A dismissed or failed browser attempt is not retried for a cooldown period
  # (login.sh sets GRAM_HOOKS_LOGIN_FORCE=1 to bypass), so an unattended
  # machine is not spammed with browser tabs on every session start.
  local now last attempt_marker
  attempt_marker="$(gram_hooks_auth_file).login-attempt"
  now="$(date +%s)"
  if [ "${GRAM_HOOKS_LOGIN_FORCE:-}" != "1" ] && [ -r "$attempt_marker" ]; then
    last="$(cat "$attempt_marker" 2>/dev/null)"
    if [ -n "$last" ] && [ "$((now - last))" -lt "${GRAM_HOOKS_LOGIN_COOLDOWN_SECONDS:-21600}" ] 2>/dev/null; then
      echo "Speakeasy hooks: browser login was attempted recently; run the plugin's hooks/login.sh to retry now." >&2
      return 1
    fi
  fi
  mkdir -p "$(dirname "$attempt_marker")" 2>/dev/null || true
  printf '%s' "$now" >"$attempt_marker" 2>/dev/null || true

  GRAM_HOOKS_LOGIN_TMPDIR="$(mktemp -d "${TMPDIR:-/tmp}/gram-hooks-login.XXXXXX")" || return 1
  local dir="$GRAM_HOOKS_LOGIN_TMPDIR"
  if ! mkfifo "$dir/fifo"; then
    rm -rf "$dir"
    GRAM_HOOKS_LOGIN_TMPDIR=""
    return 1
  fi

  local style port tries started=""
  for style in $(gram_hooks_nc_listen_styles); do
    tries=0
    while [ "$tries" -lt 2 ]; do
      tries=$((tries + 1))
      port=$(( (${RANDOM:-17} % 45000) + 20000 ))
      rm -f "$dir/query" "$dir/stop"
      gram_hooks_login_serve "$style" "$dir" "$port" &
      GRAM_HOOKS_LOGIN_SERVER_PID=$!
      GRAM_HOOKS_LOGIN_PORT="$port"
      if gram_hooks_login_probe "$port"; then
        started=1
        break 2
      fi
      gram_hooks_login_stop_server "$port" "$GRAM_HOOKS_LOGIN_SERVER_PID" "$dir"
      GRAM_HOOKS_LOGIN_SERVER_PID=""
      GRAM_HOOKS_LOGIN_PORT=""
    done
  done
  if [ -z "$started" ]; then
    rm -rf "$dir"
    GRAM_HOOKS_LOGIN_TMPDIR=""
    echo "Speakeasy hooks: could not start a localhost login listener." >&2
    gram_hooks_manual_auth_instructions "$server_url" "$project_hint"
    return 1
  fi

  local auth_url="${server_url%/}/?from_cli=true&cli_callback_url=http%3A%2F%2F127.0.0.1%3A${port}%2Fcallback&key_scope=hooks"
  if [ -n "$project_hint" ]; then
    auth_url="${auth_url}&project=${project_hint}"
  fi
  echo "Speakeasy hooks: opening your browser to connect observability hooks." >&2
  echo "If nothing opens, visit: $auth_url" >&2
  gram_hooks_open_browser "$auth_url" || true

  local waited=0
  local wait_limit="${GRAM_HOOKS_LOGIN_TIMEOUT_SECONDS:-240}"
  while [ "$waited" -lt "$wait_limit" ] && [ ! -s "$dir/query" ]; do
    sleep 1
    waited=$((waited + 1))
  done

  gram_hooks_login_stop_server "$port" "$GRAM_HOOKS_LOGIN_SERVER_PID" "$dir"
  GRAM_HOOKS_LOGIN_SERVER_PID=""
  GRAM_HOOKS_LOGIN_PORT=""

  local query=""
  if [ -r "$dir/query" ]; then
    query="$(cat "$dir/query" 2>/dev/null)"
  fi
  rm -rf "$dir"
  GRAM_HOOKS_LOGIN_TMPDIR=""
  if [ -z "$query" ]; then
    echo "Speakeasy hooks: browser login did not complete. Run the plugin's hooks/login.sh to try again." >&2
    return 1
  fi

  local api_key="" project="" email="" pair pairs
  IFS='&' read -r -a pairs <<<"$query"
  for pair in "${pairs[@]}"; do
    case "$pair" in
      api_key=*) api_key="$(gram_hooks_urldecode "${pair#api_key=}")" ;;
      project=*) project="$(gram_hooks_urldecode "${pair#project=}")" ;;
      email=*) email="$(gram_hooks_urldecode "${pair#email=}")" ;;
    esac
  done
  if [ -z "$api_key" ]; then
    echo "Speakeasy hooks: login callback did not include an API key." >&2
    return 1
  fi
  if [ -z "$project" ]; then
    project="$project_hint"
  fi
  if ! gram_hooks_write_auth "$server_url" "$api_key" "$project" "$email"; then
    echo "Speakeasy hooks: could not cache the new hooks API key." >&2
    return 1
  fi
  rm -f "$attempt_marker" 2>/dev/null || true
  echo "Speakeasy hooks: connected${email:+ as $email} (project ${project:-unset})." >&2
  return 0
}

gram_hooks_write_curl_config() {
  local api_key="$1"
  local project="$2"
  gram_hooks_cleanup_auth_config
  auth_config=""
  auth_config_arg=()
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
trap 'gram_hooks_cleanup_auth_config; gram_hooks_cleanup_login' EXIT

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
    GRAM_HOOKS_CACHED_API_KEY=""
    GRAM_HOOKS_CACHED_PROJECT=""
    GRAM_HOOKS_CACHED_EMAIL=""
    if [ "$force" != "force" ]; then
      gram_hooks_read_auth "$server_url" 2>/dev/null || true
    fi
    if [ -z "${GRAM_HOOKS_CACHED_API_KEY:-}" ]; then
      if ! gram_hooks_login "$server_url" "$project_hint"; then
        echo "Speakeasy hooks could not authenticate with Gram." >&2
        exit "$failure_exit"
      fi
      if ! gram_hooks_read_auth "$server_url" 2>/dev/null; then
        echo "Speakeasy hooks could not read Gram authentication after login." >&2
        exit "$failure_exit"
      fi
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
