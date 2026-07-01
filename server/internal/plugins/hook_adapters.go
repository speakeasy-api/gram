package plugins

import (
	"fmt"
	"strings"
)

type hookEventMapping struct {
	Native    string
	EventType string
}

type hookAdapterSpec struct {
	Platform string
	Events   []hookEventMapping
}

func hookAdapterFor(platform string) hookAdapterSpec {
	switch platform {
	case "claude":
		return hookAdapterSpec{
			Platform: "claude",
			Events: []hookEventMapping{
				{Native: "SessionStart", EventType: "session.started"},
				{Native: "ConfigChange", EventType: "session.updated"},
				{Native: "PreToolUse", EventType: "tool.requested"},
				{Native: "PostToolUse", EventType: "tool.completed"},
				{Native: "PostToolUseFailure", EventType: "tool.failed"},
				{Native: "UserPromptSubmit", EventType: "prompt.submitted"},
				{Native: "Stop", EventType: "assistant.responded"},
				{Native: "SessionEnd", EventType: "session.ended"},
				{Native: "Notification", EventType: "notification.reported"},
			},
		}
	case "cursor":
		return hookAdapterSpec{
			Platform: "cursor",
			Events: []hookEventMapping{
				{Native: "sessionStart", EventType: "session.started"},
				{Native: "beforeSubmitPrompt", EventType: "prompt.submitted"},
				{Native: "afterAgentResponse", EventType: "assistant.responded"},
				{Native: "afterAgentThought", EventType: "assistant.responded"},
				{Native: "preToolUse", EventType: "tool.requested"},
				{Native: "postToolUse", EventType: "tool.completed"},
				{Native: "postToolUseFailure", EventType: "tool.failed"},
				{Native: "beforeMCPExecution", EventType: "tool.requested"},
				{Native: "afterMCPExecution", EventType: "tool.completed"},
				{Native: "stop", EventType: "usage.reported"},
			},
		}
	case "codex":
		return hookAdapterSpec{
			Platform: "codex",
			Events: []hookEventMapping{
				{Native: "SessionStart", EventType: "session.started"},
				{Native: "PreToolUse", EventType: "tool.requested"},
				{Native: "PermissionRequest", EventType: "tool.requested"},
				{Native: "PostToolUse", EventType: "tool.completed"},
				{Native: "UserPromptSubmit", EventType: "prompt.submitted"},
				{Native: "Stop", EventType: "assistant.responded"},
			},
		}
	default:
		return hookAdapterSpec{Platform: platform, Events: nil}
	}
}

func renderHookPayloadNormalizationSnippet(platform string) string {
	spec := hookAdapterFor(platform)
	var cases strings.Builder
	for _, event := range spec.Events {
		fmt.Fprintf(&cases, "    %s:%s) printf '%s' ;;\n", spec.Platform, event.Native, event.EventType)
	}

	return fmt.Sprintf(`gram_hooks_json_escape_string() {
  awk 'BEGIN { first = 1; ORS = "" }
       {
         gsub(/\\/, "\\\\")
         gsub(/"/, "\\\"")
         gsub(/\r/, "\\r")
         gsub(/\t/, "\\t")
         if (!first) printf "\\n"
         printf "%%s", $0
         first = 0
       }'
}

gram_hooks_json_string_value() {
  local input="$1"
  local key="$2"
  local value
  if command -v jq >/dev/null 2>&1; then
    value="$(printf '%%s' "$input" | jq -r --arg key "$key" 'if type == "object" and has($key) and (.[$key] | type == "string") then .[$key] else empty end' 2>/dev/null)" || true
    if [ -n "$value" ]; then
      printf '%%s' "$value"
      return 0
    fi
  fi
  printf '%%s' "$input" | tr '\n' ' ' | sed -n 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*"\([^"\\]*\)".*/\1/p'
}

gram_hooks_json_number_value() {
  local input="$1"
  local key="$2"
  printf '%%s' "$input" | tr '\n' ' ' | sed -n 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*\([-0-9.][0-9.]*\).*/\1/p'
}

gram_hooks_json_bool_value() {
  local input="$1"
  local key="$2"
  printf '%%s' "$input" | tr '\n' ' ' | sed -n 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*\(true\|false\).*/\1/p'
}

gram_hooks_json_value() {
  local input="$1"
  local key="$2"
  local value
  if command -v jq >/dev/null 2>&1; then
    value="$(printf '%%s' "$input" | jq -c --arg key "$key" 'if type == "object" and has($key) then .[$key] else empty end' 2>/dev/null)" || true
    if [ -n "$value" ]; then
      printf '%%s' "$value"
      return 0
    fi
  fi
  gram_hooks_json_top_level_value "$input" "$key"
}

gram_hooks_json_top_level_value() {
  local input="$1"
  local key="$2"
  printf '%%s' "$input" | awk -v target="$key" '
function skip_ws(s, i, n, c) {
  n = length(s)
  while (i <= n) {
    c = substr(s, i, 1)
    if (c != " " && c != "\t" && c != "\r" && c != "\n") break
    i++
  }
  return i
}
function quoted_end(s, i, n, c, esc) {
  n = length(s)
  esc = 0
  for (i = i + 1; i <= n; i++) {
    c = substr(s, i, 1)
    if (esc) {
      esc = 0
    } else if (c == "\\") {
      esc = 1
    } else if (c == "\"") {
      return i
    }
  }
  return 0
}
function balanced_end(s, i, open_ch, close_ch, n, c, depth, esc, in_str) {
  n = length(s)
  depth = 0
  esc = 0
  in_str = 0
  for (; i <= n; i++) {
    c = substr(s, i, 1)
    if (in_str) {
      if (esc) {
        esc = 0
      } else if (c == "\\") {
        esc = 1
      } else if (c == "\"") {
        in_str = 0
      }
    } else if (c == "\"") {
      in_str = 1
    } else if (c == open_ch) {
      depth++
    } else if (c == close_ch) {
      depth--
      if (depth == 0) return i
    }
  }
  return 0
}
function bare_end(s, i, n, c) {
  n = length(s)
  for (; i <= n; i++) {
    c = substr(s, i, 1)
    if (c == "," || c == "}" || c == "\n" || c == "\r") return i - 1
  }
  return n
}
function trim(s) {
  sub(/^[[:space:]]+/, "", s)
  sub(/[[:space:]]+$/, "", s)
  return s
}
{
  json = json $0 "\n"
}
END {
  n = length(json)
  for (i = 1; i <= n; i++) {
    if (substr(json, i, 1) != "\"") continue
    key_end = quoted_end(json, i)
    if (key_end == 0) exit
    raw_key = substr(json, i + 1, key_end - i - 1)
    colon = skip_ws(json, key_end + 1)
    if (substr(json, colon, 1) != ":") {
      i = key_end
      continue
    }
    if (raw_key != target) {
      i = key_end
      continue
    }
    start = skip_ws(json, colon + 1)
    c = substr(json, start, 1)
    if (c == "\"") {
      stop = quoted_end(json, start)
    } else if (c == "{") {
      stop = balanced_end(json, start, "{", "}")
    } else if (c == "[") {
      stop = balanced_end(json, start, "[", "]")
    } else {
      stop = bare_end(json, start)
    }
    if (stop > 0) print trim(substr(json, start, stop - start + 1))
    exit
  }
}'
}

gram_hooks_json_string_member() {
  local key="$1"
  local value="$2"
  [ -n "$value" ] || return 0
  local escaped
  escaped=$(printf '%%s' "$value" | gram_hooks_json_escape_string)
  printf '"%%s":"%%s"' "$key" "$escaped"
}

gram_hooks_json_raw_or_string() {
  local input="$1"
  local trimmed
  trimmed="$(printf '%%s' "$input" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  case "$trimmed" in
    \{* | \[* | true | false | null) printf '%%s' "$trimmed" ;;
    *)
      local escaped
      escaped=$(printf '%%s' "$input" | gram_hooks_json_escape_string)
      printf '"%%s"' "$escaped"
      ;;
  esac
}

gram_hooks_join_members() {
  local joined=""
  local member
  for member in "$@"; do
    [ -n "$member" ] || continue
    if [ -n "$joined" ]; then
      joined="${joined},"
    fi
    joined="${joined}${member}"
  done
  printf '%%s' "$joined"
}

gram_hooks_normalized_event_type() {
  local native="$1"
  case "%s:$native" in
%s    *) printf '%%s' "$native" ;;
  esac
}

gram_hooks_native_event_name() {
  local payload="$1"
  local native
  native="$(gram_hooks_json_string_value "$payload" "hook_event_name")"
  [ -n "$native" ] || native="$(gram_hooks_json_string_value "$payload" "event_name")"
  [ -n "$native" ] || native="$(gram_hooks_json_string_value "$payload" "event")"
  printf '%%s' "$native"
}

gram_hooks_session_id() {
  local payload="$1"
  local id
  id="$(gram_hooks_json_string_value "$payload" "session_id")"
  [ -n "$id" ] || id="$(gram_hooks_json_string_value "$payload" "conversation_id")"
  printf '%%s' "$id"
}

gram_hooks_turn_id() {
  local payload="$1"
  local id
  id="$(gram_hooks_json_string_value "$payload" "generation_id")"
  [ -n "$id" ] || id="$(gram_hooks_json_string_value "$payload" "tool_use_id")"
  printf '%%s' "$id"
}

gram_hooks_tool_output_value() {
  local payload="$1"
  local output
  output="$(gram_hooks_json_value "$payload" "tool_output")"
  [ -n "$output" ] || output="$(gram_hooks_json_value "$payload" "tool_response")"
  [ -n "$output" ] || output="$(gram_hooks_json_value "$payload" "result_json")"
  printf '%%s' "$output"
}

gram_hooks_skill_name() {
  local payload="$1"
  local skill tool_name
  skill="$(gram_hooks_json_string_value "$payload" "skill_name")"
  [ -n "$skill" ] || skill="$(gram_hooks_json_string_value "$payload" "skill")"
  tool_name="$(gram_hooks_json_string_value "$payload" "tool_name")"
  if [ -z "$skill" ] && [ "$tool_name" = "Skill" ]; then
    if command -v jq >/dev/null 2>&1; then
      skill="$(printf '%%s' "$payload" | jq -r '(.tool_input.skill // .tool_input.name // empty) | select(type == "string")' 2>/dev/null)"
    fi
    [ -n "$skill" ] || skill="$(gram_hooks_json_string_value "$payload" "name")"
  fi
  printf '%%s' "$skill"
}

gram_hooks_mcp_server_from_tool_name() {
  local tool_name="$1"
  local rest
  case "$tool_name" in
    mcp__*__*)
      rest="${tool_name#mcp__}"
      printf '%%s' "${rest%%%%__*}"
      ;;
  esac
}

gram_hooks_mcp_server_from_payload() {
  local payload="$1"
  local tool_name server
  server="$(gram_hooks_first_string "$payload" "mcp_server_name" "server_name")"
  if [ -n "$server" ]; then
    printf '%%s' "$server"
    return 0
  fi
  if command -v jq >/dev/null 2>&1; then
    server="$(printf '%%s' "$payload" | jq -r '(.tool_input.server // empty) | select(type == "string")' 2>/dev/null)" || true
    if [ -n "$server" ]; then
      printf '%%s' "$server"
      return 0
    fi
  fi
  tool_name="$(gram_hooks_json_string_value "$payload" "tool_name")"
  gram_hooks_mcp_server_from_tool_name "$tool_name"
}

gram_hooks_codex_mcp_metadata() {
  local server="$1"
  [ "%s" = "codex" ] || return 0
  [ -n "$server" ] || return 0
  command -v codex >/dev/null 2>&1 || return 0
  command -v jq >/dev/null 2>&1 || return 0
  codex mcp list --json 2>/dev/null | jq -r --arg server "$server" '
    def sanitize: gsub("[^A-Za-z0-9_]"; "_");
    def clean: map(select(. != null and . != "") | tostring);
    def redact_args: reduce .[] as $arg ({out: [], next: false};
      if .next then {out: (.out + ["***"]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*="; "i")) then
        {out: (.out + [($arg | sub("=.*"; "=***"))]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*$"; "i")) then
        {out: (.out + [$arg]), next: true}
      elif ($arg | test("^(--?[^=]*=)?(authorization|proxy-authorization|cookie|x-api-key) *:"; "i")) then
        {out: (.out + [($arg | sub(":.*"; ": ***"))]), next: false}
      elif ($arg | test("bearer +[^ ]"; "i")) then
        {out: (.out + ["***"]), next: false}
      elif ($arg | test("://[^/@]*@|^(sk-|ghp_|gho_|github_pat_|xox[a-z]-|glpat-)")) then
        {out: (.out + ["***"]), next: false}
      else {out: (.out + [$arg]), next: false} end) | .out;
    map(select(.name == $server or (.name | sanitize) == $server)) | .[0] as $m |
    if $m == null then
      empty
    else
      "url=\($m.transport.url // "")",
      "command=\(if $m.transport.type == "stdio" then ((([$m.transport.command] | clean) + (($m.transport.args // []) | clean | redact_args)) | join(" ")) else "" end)"
    end
  ' | head -n 2
}

gram_hooks_mcp_metadata_from_file() {
  local file="$1"
  local server="$2"
  [ -f "$file" ] || return 0
  [ -n "$server" ] || return 0
  command -v jq >/dev/null 2>&1 || return 0
  jq -r --arg server "$server" '
    def clean: map(select(. != null and . != "") | tostring);
    def redact_args: reduce .[] as $arg ({out: [], next: false};
      if .next then {out: (.out + ["***"]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*="; "i")) then
        {out: (.out + [($arg | sub("=.*"; "=***"))]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*$"; "i")) then
        {out: (.out + [$arg]), next: true}
      elif ($arg | test("^(--?[^=]*=)?(authorization|proxy-authorization|cookie|x-api-key) *:"; "i")) then
        {out: (.out + [($arg | sub(":.*"; ": ***"))]), next: false}
      elif ($arg | test("bearer +[^ ]"; "i")) then
        {out: (.out + ["***"]), next: false}
      elif ($arg | test("://[^/@]*@|^(sk-|ghp_|gho_|github_pat_|xox[a-z]-|glpat-)")) then
        {out: (.out + ["***"]), next: false}
      else {out: (.out + [$arg]), next: false} end) | .out;
    (.mcpServers[$server] // empty) as $m |
    if $m == null then
      empty
    else
      "url=\($m.url // "")",
      "command=\(if ($m.command // "") != "" then ((([$m.command] | clean) + (($m.args // []) | clean | redact_args)) | join(" ")) else "" end)"
    end
  ' "$file" 2>/dev/null | head -n 2
}

gram_hooks_local_mcp_metadata() {
  local server="$1"
  local metadata
  for file in ".mcp.json" ".cursor/mcp.json"; do
    metadata="$(gram_hooks_mcp_metadata_from_file "$file" "$server")"
    if [ -n "$metadata" ]; then
      printf '%%s\n' "$metadata"
      return 0
    fi
  done
}

gram_hooks_cursor_prompt_state_path() {
  local session_id="$1"
  local server_url_arg="$2"
  local project_slug_arg="$3"
  local state_home state_dir safe_id safe_install
  [ -n "$session_id" ] || return 1
  state_home="${XDG_STATE_HOME:-${HOME}/.local/state}"
  state_dir="${state_home}/gram/hooks/cursor-prompts"
  mkdir -p "$state_dir" 2>/dev/null || return 1
  chmod 700 "${state_home}/gram" "${state_home}/gram/hooks" "$state_dir" 2>/dev/null || true
  safe_install="$(printf '%%s' "$server_url_arg" | tr -c 'A-Za-z0-9_.-' '_')__$(printf '%%s' "$project_slug_arg" | tr -c 'A-Za-z0-9_.-' '_')"
  safe_id="$(printf '%%s' "$session_id" | tr -c 'A-Za-z0-9_.-' '_')"
  printf '%%s/%%s__%%s.seen' "$state_dir" "$safe_install" "$safe_id"
}

gram_hooks_cursor_mark_prompt_submitted() {
  local payload="$1"
  local server_url_arg="$2"
  local project_slug_arg="$3"
  local session_id state_path
  session_id="$(gram_hooks_session_id "$payload")"
  state_path="$(gram_hooks_cursor_prompt_state_path "$session_id" "$server_url_arg" "$project_slug_arg")" || return 0
  printf 'seen\n' >"$state_path" 2>/dev/null || true
}

gram_hooks_base64_decode() {
  if base64 --decode >/dev/null 2>&1 <<<'dGVzdA=='; then
    base64 --decode
    return
  fi
  base64 -D
}

gram_hooks_cursor_clean_transcript_prompt() {
  awk '
    NR == 1 && $0 == "<user_query>" { next }
    $0 == "</user_query>" { next }
    { print }
  ' | sed -e :a -e '/^[[:space:]]*$/{$d;N;ba' -e '}'
}

gram_hooks_cursor_transcript_prompt() {
  local payload="$1"
  local transcript_path encoded
  transcript_path="$(gram_hooks_json_string_value "$payload" "transcript_path")"
  [ -n "$transcript_path" ] || transcript_path="${CURSOR_TRANSCRIPT_PATH:-}"
  [ -n "$transcript_path" ] && [ -r "$transcript_path" ] || return 0
  command -v jq >/dev/null 2>&1 || return 0
  command -v base64 >/dev/null 2>&1 || return 0

  encoded="$(jq -r '
    select(.role == "user")
    | [.message.content[]? | select(.type == "text") | .text]
    | join("\n")
    | @base64
  ' "$transcript_path" 2>/dev/null | sed -n '1p')" || true
  [ -n "$encoded" ] || return 0
  printf '%%s' "$encoded" | gram_hooks_base64_decode 2>/dev/null | gram_hooks_cursor_clean_transcript_prompt
}

gram_hooks_cursor_backfill_prompt_if_missing() {
  local payload="$1"
  local hostname="$2"
  local server_url_arg="$3"
  local project_slug_arg="$4"
  local session_id state_path prompt prompt_payload prompt_members canonical_prompt http_code

  session_id="$(gram_hooks_session_id "$payload")"
  state_path="$(gram_hooks_cursor_prompt_state_path "$session_id" "$server_url_arg" "$project_slug_arg")" || return 0
  [ ! -f "$state_path" ] || return 0

  prompt="$(gram_hooks_cursor_transcript_prompt "$payload")"
  [ -n "$prompt" ] || return 0

  prompt_members=$(gram_hooks_join_members \
    "$(gram_hooks_json_string_member "hook_event_name" "beforeSubmitPrompt")" \
    "$(gram_hooks_json_string_member "prompt" "$prompt")" \
    "$(gram_hooks_json_string_member "conversation_id" "$(gram_hooks_json_string_value "$payload" "conversation_id")")" \
    "$(gram_hooks_json_string_member "generation_id" "$(gram_hooks_json_string_value "$payload" "generation_id")")" \
    "$(gram_hooks_json_string_member "session_id" "$session_id")" \
    "$(gram_hooks_json_string_member "cursor_version" "$(gram_hooks_json_string_value "$payload" "cursor_version")")" \
    "$(gram_hooks_json_string_member "model" "$(gram_hooks_json_string_value "$payload" "model")")" \
    "$(gram_hooks_json_string_member "transcript_path" "$(gram_hooks_json_string_value "$payload" "transcript_path")")")
  prompt_payload="{$prompt_members}"
  canonical_prompt="$(gram_hooks_build_canonical_payload "$prompt_payload" "$hostname")"

  unset GRAM_IDEMPOTENCY_TOKEN
  gram_hooks_post_authenticated "$server_url_arg" "$canonical_prompt" 10 "$project_slug_arg" 2
  unset GRAM_IDEMPOTENCY_TOKEN
  http_code="$GRAM_HTTP_CODE"
  if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
    printf 'seen\n' >"$state_path" 2>/dev/null || true
  fi
}

gram_hooks_build_canonical_payload() {
  local payload="$1"
  local hostname="$2"
  local native event_type session_id turn_id source_members session_members event_members data_members raw_json
  native="$(gram_hooks_native_event_name "$payload")"
  event_type="$(gram_hooks_normalized_event_type "$native")"
  [ -n "$event_type" ] || event_type="$native"
  [ -n "$event_type" ] || event_type="session.updated"
  if [ -n "$(gram_hooks_skill_name "$payload")" ]; then
    event_type="skill.activated"
  fi

  session_id="$(gram_hooks_session_id "$payload")"
  turn_id="$(gram_hooks_turn_id "$payload")"

  source_members=$(gram_hooks_join_members \
    "$(gram_hooks_json_string_member "adapter" "%s")" \
    "$(gram_hooks_json_string_member "adapter_version" "$(gram_hooks_first_string "$payload" "cursor_version" "codex_version" "version")")" \
    "$(gram_hooks_json_string_member "raw_event_name" "$native")" \
    "$(gram_hooks_json_string_member "hostname" "$hostname")")
  session_members=$(gram_hooks_join_members \
    "$(gram_hooks_json_string_member "id" "$session_id")" \
    "$(gram_hooks_json_string_member "turn_id" "$turn_id")" \
    "$(gram_hooks_json_string_member "cwd" "$(gram_hooks_json_string_value "$payload" "cwd")")" \
    "$(gram_hooks_json_string_member "model" "$(gram_hooks_json_string_value "$payload" "model")")")
  event_members=$(gram_hooks_join_members \
    "$(gram_hooks_json_string_member "type" "$event_type")" \
    "$(gram_hooks_json_string_member "occurred_at" "$(gram_hooks_json_string_value "$payload" "occurred_at")")")

  data_members="$(gram_hooks_canonical_data_members "$payload" "$event_type")"
  raw_json="$(gram_hooks_json_raw_or_string "$payload")"

  printf '{"schema_version":"hook.ingest.v1","source":{%%s},"event":{%%s}' "$source_members" "$event_members"
  if [ -n "$session_members" ]; then
    printf ',"session":{%%s}' "$session_members"
  fi
  if [ -n "$data_members" ]; then
    printf ',"data":{%%s}' "$data_members"
  fi
  printf ',"raw":%%s}' "$raw_json"
}

gram_hooks_canonical_data_members() {
  local payload="$1"
  local event_type="$2"
  local prompt text message_duration_ms tool_name tool_id tool_input tool_output tool_error is_interrupt permission_type duration_ms status mcp_members usage_members message_members="" skill_name notification_members data_members

  prompt="$(gram_hooks_json_string_value "$payload" "prompt")"
  if [ -n "$prompt" ]; then
    prompt="\"prompt\":{\"text\":\"$(printf '%%s' "$prompt" | gram_hooks_json_escape_string)\"}"
  fi

  text="$(gram_hooks_json_string_value "$payload" "last_assistant_message")"
  [ -n "$text" ] || text="$(gram_hooks_json_string_value "$payload" "text")"
  message_duration_ms="$(gram_hooks_json_number_value "$payload" "duration_ms")"
  if [ -n "$text" ]; then
    local message_data_members
    message_data_members=$(gram_hooks_join_members \
      "$(gram_hooks_json_string_member "text" "$text")" \
      "$(gram_hooks_json_string_member "role" "assistant")" \
      "$(gram_hooks_number_member "duration_ms" "$message_duration_ms")")
    message_members="\"message\":{$message_data_members}"
  fi

  tool_name="$(gram_hooks_json_string_value "$payload" "tool_name")"
  tool_id="$(gram_hooks_json_string_value "$payload" "tool_use_id")"
  tool_input="$(gram_hooks_json_value "$payload" "tool_input")"
  tool_output="$(gram_hooks_tool_output_value "$payload")"
  tool_error="$(gram_hooks_json_value "$payload" "error")"
  is_interrupt="$(gram_hooks_json_bool_value "$payload" "is_interrupt")"
  permission_type="$(gram_hooks_json_string_value "$payload" "permission_type")"
  duration_ms="$(gram_hooks_json_number_value "$payload" "duration_ms")"
  [ -n "$duration_ms" ] || duration_ms="$(gram_hooks_json_number_value "$payload" "duration")"
  status="$(gram_hooks_json_string_value "$payload" "status")"
  if [ -n "$tool_name" ] || [ "$event_type" = "tool.requested" ] || [ "$event_type" = "tool.completed" ] || [ "$event_type" = "tool.failed" ]; then
    local tool_members tool_input_member="" tool_output_member="" tool_error_member=""
    if [ -n "$tool_input" ]; then
      tool_input_member="\"input\":$tool_input"
    fi
    if [ -n "$tool_output" ]; then
      tool_output_member="\"output\":$tool_output"
    fi
    if [ -n "$tool_error" ]; then
      tool_error_member="\"error\":$tool_error"
    fi
    tool_members=$(gram_hooks_join_members \
      "$(gram_hooks_json_string_member "id" "$tool_id")" \
      "$(gram_hooks_json_string_member "name" "$tool_name")" \
      "$tool_input_member" \
      "$tool_output_member" \
      "$tool_error_member" \
      "$(gram_hooks_json_string_member "permission_type" "$permission_type")" \
      "$(gram_hooks_number_member "duration_ms" "$duration_ms")" \
      "$(gram_hooks_json_string_member "status" "$status")")
    if [ -n "$is_interrupt" ]; then
      tool_members="${tool_members},\"is_interrupt\":$is_interrupt"
    fi
    tool_members="\"tool_call\":{$tool_members}"
  else
    tool_members=""
  fi

  local mcp_server_name mcp_server_identity mcp_url mcp_command mcp_metadata
  mcp_server_name="$(gram_hooks_mcp_server_from_payload "$payload")"
  mcp_server_identity="$(gram_hooks_first_string "$payload" "server_identity" "mcp_server_name" "command")"
  [ -n "$mcp_server_identity" ] || mcp_server_identity="$mcp_server_name"
  mcp_url="$(gram_hooks_first_string "$payload" "url" "mcp_server_url")"
  mcp_command="$(gram_hooks_json_string_value "$payload" "command")"
  if [ -n "$mcp_server_name" ] && { [ -z "$mcp_url" ] || [ -z "$mcp_command" ]; }; then
    mcp_metadata="$(gram_hooks_local_mcp_metadata "$mcp_server_name")"
    [ -n "$mcp_metadata" ] || mcp_metadata="$(gram_hooks_codex_mcp_metadata "$mcp_server_name")"
    if [ -n "$mcp_metadata" ]; then
      [ -n "$mcp_url" ] || mcp_url="$(printf '%%s\n' "$mcp_metadata" | sed -n 's/^url=//p')"
      [ -n "$mcp_command" ] || mcp_command="$(printf '%%s\n' "$mcp_metadata" | sed -n 's/^command=//p')"
    fi
  fi

  mcp_members=$(gram_hooks_join_members \
    "$(gram_hooks_json_string_member "server_name" "$mcp_server_name")" \
    "$(gram_hooks_json_string_member "server_identity" "$mcp_server_identity")" \
    "$(gram_hooks_json_string_member "url" "$mcp_url")" \
    "$(gram_hooks_json_string_member "command" "$mcp_command")" \
    "$(gram_hooks_json_string_member "result_json" "$(gram_hooks_json_string_value "$payload" "result_json")")")
  if [ -n "$mcp_members" ]; then
    mcp_members="\"mcp\":{$mcp_members}"
  fi

  usage_members=$(gram_hooks_join_members \
    "$(gram_hooks_number_member "input_tokens" "$(gram_hooks_json_number_value "$payload" "input_tokens")")" \
    "$(gram_hooks_number_member "output_tokens" "$(gram_hooks_json_number_value "$payload" "output_tokens")")" \
    "$(gram_hooks_number_member "cache_read_tokens" "$(gram_hooks_json_number_value "$payload" "cache_read_tokens")")" \
    "$(gram_hooks_number_member "cache_write_tokens" "$(gram_hooks_json_number_value "$payload" "cache_write_tokens")")" \
    "$(gram_hooks_number_member "cost" "$(gram_hooks_json_number_value "$payload" "cost")")" \
    "$(gram_hooks_number_member "loop_count" "$(gram_hooks_json_number_value "$payload" "loop_count")")" \
    "$(gram_hooks_json_string_member "status" "$(gram_hooks_json_string_value "$payload" "status")")")
  if [ -n "$usage_members" ]; then
    usage_members="\"usage\":{$usage_members}"
  fi

  skill_name="$(gram_hooks_skill_name "$payload")"
  if [ -n "$skill_name" ]; then
    skill_name="\"skill\":{\"name\":\"$(printf '%%s' "$skill_name" | gram_hooks_json_escape_string)\"}"
  fi

  notification_members=$(gram_hooks_join_members \
    "$(gram_hooks_json_string_member "type" "$(gram_hooks_json_string_value "$payload" "notification_type")")" \
    "$(gram_hooks_json_string_member "title" "$(gram_hooks_json_string_value "$payload" "title")")" \
    "$(gram_hooks_json_string_member "message" "$(gram_hooks_json_string_value "$payload" "message")")")
  if [ -n "$notification_members" ]; then
    notification_members="\"notification\":{$notification_members}"
  fi

  data_members=$(gram_hooks_join_members "$prompt" "$message_members" "$tool_members" "$mcp_members" "$usage_members" "$skill_name" "$notification_members")
  printf '%%s' "$data_members"
}

gram_hooks_first_string() {
  local payload="$1"
  shift
  local key value
  for key in "$@"; do
    value="$(gram_hooks_json_string_value "$payload" "$key")"
    if [ -n "$value" ]; then
      printf '%%s' "$value"
      return 0
    fi
  done
}

gram_hooks_number_member() {
  local key="$1"
  local value="$2"
  [ -n "$value" ] || return 0
  printf '"%%s":%%s' "$key" "$value"
}

gram_hooks_decision_message() {
  local body="$1"
  local msg
  msg="$(gram_hooks_json_string_value "$body" "message")"
  [ -n "$msg" ] || msg="$(gram_hooks_json_string_value "$body" "reason")"
  printf '%%s' "$msg"
}

gram_hooks_provider_response() {
  local platform="$1"
  local native="$2"
  local body="$3"
  local decision message escaped
  decision="$(gram_hooks_json_string_value "$body" "decision")"
  message="$(gram_hooks_decision_message "$body")"
  escaped="$(printf '%%s' "${message:-Speakeasy blocked this hook}" | gram_hooks_json_escape_string)"

  case "$platform" in
    claude)
      if [ "$decision" = "deny" ]; then
        if [ "$native" = "PreToolUse" ]; then
          printf '{"systemMessage":"%%s","hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%%s"}}' "$escaped" "$escaped"
        else
          printf '{"decision":"block","reason":"%%s"}' "$escaped"
        fi
      elif [ "$native" = "PreToolUse" ]; then
        printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'
      elif [ "$native" = "SessionStart" ]; then
        printf '{"continue":true}'
      else
        printf '{}'
      fi
      ;;
    cursor)
      if [ "$decision" = "deny" ]; then
        printf '{"permission":"deny","user_message":"%%s","agent_message":"%%s"}' "$escaped" "$escaped"
      elif [ "$native" = "preToolUse" ] || [ "$native" = "beforeMCPExecution" ]; then
        printf '{"permission":"allow"}'
      else
        printf '{}'
      fi
      ;;
    *)
      printf '%%s' "$body"
      ;;
  esac
}
`, spec.Platform, cases.String(), spec.Platform, spec.Platform)
}
