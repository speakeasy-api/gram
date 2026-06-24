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
				{Native: "SessionStart", EventType: "session_start"},
				{Native: "ConfigChange", EventType: "config_change"},
				{Native: "PreToolUse", EventType: "before_tool_use"},
				{Native: "PostToolUse", EventType: "after_tool_use"},
				{Native: "PostToolUseFailure", EventType: "after_tool_use_failure"},
				{Native: "UserPromptSubmit", EventType: "user_prompt_submit"},
				{Native: "Stop", EventType: "stop"},
				{Native: "SessionEnd", EventType: "session_end"},
				{Native: "Notification", EventType: "notification"},
			},
		}
	case "cursor":
		return hookAdapterSpec{
			Platform: "cursor",
			Events: []hookEventMapping{
				{Native: "sessionStart", EventType: "session_start"},
				{Native: "beforeSubmitPrompt", EventType: "user_prompt_submit"},
				{Native: "afterAgentResponse", EventType: "after_agent_response"},
				{Native: "afterAgentThought", EventType: "after_agent_thought"},
				{Native: "preToolUse", EventType: "before_tool_use"},
				{Native: "postToolUse", EventType: "after_tool_use"},
				{Native: "postToolUseFailure", EventType: "after_tool_use_failure"},
				{Native: "beforeMCPExecution", EventType: "before_mcp_execution"},
				{Native: "afterMCPExecution", EventType: "after_mcp_execution"},
				{Native: "stop", EventType: "stop"},
			},
		}
	case "codex":
		return hookAdapterSpec{
			Platform: "codex",
			Events: []hookEventMapping{
				{Native: "SessionStart", EventType: "session_start"},
				{Native: "PreToolUse", EventType: "before_tool_use"},
				{Native: "PostToolUse", EventType: "after_tool_use"},
				{Native: "PermissionRequest", EventType: "permission_request"},
				{Native: "UserPromptSubmit", EventType: "user_prompt_submit"},
				{Native: "Stop", EventType: "stop"},
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

	return fmt.Sprintf(`gram_hooks_json_string_value() {
  local input="$1"
  local key="$2"
  printf '%%s' "$input" | tr '\n' ' ' | sed -n 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*"\([^"\\]*\)".*/\1/p'
}

gram_hooks_payload_has_json_key() {
  case "$1" in
    *\""$2"\"*) return 0 ;;
    *) return 1 ;;
  esac
}

gram_hooks_add_json_string_field() {
  local input="$1"
  local key="$2"
  local value="$3"
  local trimmed
  trimmed="$(printf '%%s' "$input" | sed 's/^[[:space:]]*//')"
  case "$trimmed" in
    \{*) printf '{"%%s":"%%s",%%s' "$key" "$value" "${trimmed#\{}" ;;
    *) printf '%%s' "$input" ;;
  esac
}

gram_hooks_normalized_event_type() {
  local native="$1"
  case "%s:$native" in
%s    *) printf '%%s' "$native" ;;
  esac
}

if ! gram_hooks_payload_has_json_key "$payload" "event_type"; then
  native_event="$(gram_hooks_json_string_value "$payload" "hook_event_name")"
  if [ -n "$native_event" ]; then
    normalized_event="$(gram_hooks_normalized_event_type "$native_event")"
    if [ -n "$normalized_event" ]; then
      payload="$(gram_hooks_add_json_string_field "$payload" "event_type" "$normalized_event")"
    fi
  fi
fi`, spec.Platform, cases.String())
}
