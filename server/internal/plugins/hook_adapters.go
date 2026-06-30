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
  command -v jq >/dev/null 2>&1 || return 0
  printf '%%s' "$input" | jq -c --arg key "$key" 'if type == "object" and has($key) then .[$key] else empty end' 2>/dev/null
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
  local prompt text message_duration_ms tool_name tool_id tool_input tool_output tool_error is_interrupt permission_type duration_ms status mcp_members usage_members message_members skill_name notification_members data_members

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
    local tool_members tool_input_member tool_output_member tool_error_member
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

  mcp_members=$(gram_hooks_join_members \
    "$(gram_hooks_json_string_member "server_name" "$(gram_hooks_first_string "$payload" "mcp_server_name" "server_name")")" \
    "$(gram_hooks_json_string_member "server_identity" "$(gram_hooks_first_string "$payload" "server_identity" "mcp_server_name" "command")")" \
    "$(gram_hooks_json_string_member "url" "$(gram_hooks_first_string "$payload" "url" "mcp_server_url")")" \
    "$(gram_hooks_json_string_member "command" "$(gram_hooks_json_string_value "$payload" "command")")" \
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
`, spec.Platform, cases.String(), spec.Platform)
}
