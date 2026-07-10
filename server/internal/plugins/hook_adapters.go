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
				{Native: "afterAgentThought", EventType: "assistant.thought"},
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
  awk 'BEGIN {
         first = 1; ORS = ""
         # Every remaining C0 control character must be escaped or the
         # payload is invalid JSON and the whole ingest post fails; ANSI
         # escapes in tool output and prompts hit this in practice. NUL
         # cannot survive shell command substitution, so start at 1.
         for (i = 1; i < 32; i++) ctrl[sprintf("%%c", i)] = sprintf("\\u%%04x", i)
       }
       {
         gsub(/\\/, "\\\\")
         gsub(/"/, "\\\"")
         if ($0 ~ /[[:cntrl:]]/) {
           n = length($0); out = ""
           for (i = 1; i <= n; i++) {
             c = substr($0, i, 1)
             out = out ((c in ctrl) ? ctrl[c] : c)
           }
           $0 = out
         }
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
  # Only the balanced top-level extractor is safe here: a greedy whole-payload
  # scan would also match nested keys (e.g. tool_input.url or
  # tool_input.command) and misclassify ordinary tool calls as MCP.
  value="$(gram_hooks_json_top_level_value "$input" "$key")"
  case "$value" in
    \"*\")
      value="${value#\"}"
      printf '%%s' "${value%%\"}" | gram_hooks_json_decode_string
      ;;
  esac
}

# gram_hooks_json_decode_string decodes JSON string escapes from a token body
# (surrounding quotes already stripped). Unicode \uXXXX escapes are passed
# through literally rather than dropping the whole value.
gram_hooks_json_decode_string() {
  awk 'BEGIN { ORS = "" }
       {
         if (NR > 1) printf "\n"
         s = $0
         out = ""
         n = length(s)
         i = 1
         while (i <= n) {
           c = substr(s, i, 1)
           if (c == "\\" && i < n) {
             e = substr(s, i + 1, 1)
             if (e == "n") out = out "\n"
             else if (e == "t") out = out "\t"
             else if (e == "r") out = out "\r"
             else if (e == "b") out = out "\b"
             else if (e == "f") out = out "\f"
             else if (e == "\"") out = out "\""
             else if (e == "\\") out = out "\\"
             else if (e == "/") out = out "/"
             else out = out c e
             i += 2
           } else {
             out = out c
             i++
           }
         }
         printf "%%s", out
       }'
}

gram_hooks_json_number_value() {
  local input="$1"
  local key="$2"
  # Top-level only: a greedy scan would also match same-named nested keys.
  # The shape check covers the JSON number grammar including exponents (a
  # dropped 1e3 would silently lose token counts or costs); POSIX BRE only,
  # so no \| alternation (BSD sed).
  gram_hooks_json_top_level_value "$input" "$key" | sed -n 's/^\(-\{0,1\}[0-9][0-9]*\(\.[0-9][0-9]*\)\{0,1\}\([eE][+-]\{0,1\}[0-9][0-9]*\)\{0,1\}\)$/\1/p'
}

gram_hooks_json_bool_value() {
  local input="$1"
  local key="$2"
  local value
  value="$(gram_hooks_json_top_level_value "$input" "$key")"
  case "$value" in
    true | false) printf '%%s' "$value" ;;
  esac
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
  # Depth-tracked scan: only keys of the root object (depth 1) may match, so
  # a same-named key nested inside tool_input can never be returned.
  n = length(json)
  depth = 0
  i = 1
  while (i <= n) {
    c = substr(json, i, 1)
    if (c == "\"") {
      key_end = quoted_end(json, i)
      if (key_end == 0) exit
      if (depth == 1) {
        raw_key = substr(json, i + 1, key_end - i - 1)
        colon = skip_ws(json, key_end + 1)
        if (substr(json, colon, 1) == ":" && raw_key == target) {
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
      }
      i = key_end + 1
      continue
    }
    if (c == "{" || c == "[") {
      depth++
    } else if (c == "}" || c == "]") {
      depth--
    }
    i++
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

# gram_hooks_codex_synth_state_path stores the in-flight synthetic tool ids
# for one (install, session, tool) tuple. Scoped by the hook's server URL and
# project (script-level globals in every generated sender, mirroring the
# cursor prompt marker) so distinct installs never consume each other's state.
gram_hooks_codex_synth_state_path() {
  local session_id="$1"
  local tool_name="$2"
  local state_home state_dir safe_id safe_install
  [ -n "$session_id" ] || return 1
  state_home="${XDG_STATE_HOME:-${HOME}/.local/state}"
  state_dir="${state_home}/gram/hooks/codex-tools"
  mkdir -p "$state_dir" 2>/dev/null || return 1
  chmod 700 "${state_home}/gram" "${state_home}/gram/hooks" "$state_dir" 2>/dev/null || true
  safe_install="$(printf '%%s' "${server_url:-}" | tr -c 'A-Za-z0-9_.-' '_')__$(printf '%%s' "${project_slug:-}" | tr -c 'A-Za-z0-9_.-' '_')"
  safe_id="$(printf '%%s' "${session_id}__${tool_name}" | tr -c 'A-Za-z0-9_.-' '_')"
  printf '%%s/%%s__%%s.ids' "$state_dir" "$safe_install" "$safe_id"
}

# gram_hooks_codex_synth_pop_id removes and prints the oldest queued id.
gram_hooks_codex_synth_pop_id() {
  local state_path="$1"
  local first
  [ -r "$state_path" ] || return 0
  first="$(sed -n '1p' "$state_path" 2>/dev/null)"
  if sed '1d' "$state_path" >"$state_path.tmp" 2>/dev/null; then
    mv "$state_path.tmp" "$state_path" 2>/dev/null || rm -f "$state_path.tmp"
  fi
  [ -s "$state_path" ] || rm -f "$state_path" 2>/dev/null
  printf '%%s' "$first"
}

# gram_hooks_synth_tool_id derives a stable tool-call id when the provider
# omits tool_use_id (common for Cursor MCP and some pre/post tool events) so
# before/after records still correlate instead of collapsing into a
# session-derived identity. Mirrors the legacy server-side derivation:
# hash(conversation|generation|tool|input).
#
# Codex carries no generation id and omits tool_input on PostToolUse, so
# neither hashing with input (request/result ids diverge) nor without it
# (same-tool requests collide) works from payload content alone: the request
# id is remembered locally and replayed on the matching completion.
gram_hooks_synth_tool_id() {
  local payload="$1"
  local tool_name="$2"
  local tool_input="$3"
  local event_type="$4"
  local conv_id gen_id material hash state_path=""
  conv_id="$(gram_hooks_json_string_value "$payload" "conversation_id")"
  [ -n "$conv_id" ] || conv_id="$(gram_hooks_json_string_value "$payload" "session_id")"
  gen_id="$(gram_hooks_json_string_value "$payload" "generation_id")"
  tool_name="${tool_name#MCP:}"
  if [ "%s" = "codex" ]; then
    state_path="$(gram_hooks_codex_synth_state_path "${conv_id}|${gen_id}" "$tool_name")" || state_path=""
    case "$event_type" in
      tool.completed | tool.failed)
        # Completions run through the async sender and can lag behind the
        # next same-tool request, so ids are queued FIFO rather than held in
        # a single overwritten slot.
        if [ -n "$state_path" ]; then
          hash="$(gram_hooks_codex_synth_pop_id "$state_path")"
          if [ -n "$hash" ]; then
            printf '%%s' "$hash"
            return 0
          fi
        fi
        # No remembered request id (state unavailable or request never seen):
        # fall back to the input-less hash rather than diverging on input the
        # completion does not carry.
        tool_input=""
        ;;
    esac
  fi
  if [ -z "$conv_id" ] && [ -z "$gen_id" ] && [ -z "$tool_name" ] && [ -z "$tool_input" ]; then
    return 0
  fi
  material="${conv_id}|${gen_id}|${tool_name}|${tool_input}"
  if command -v sha256sum >/dev/null 2>&1; then
    hash="$(printf '%%s' "$material" | sha256sum | cut -c1-16)"
  elif command -v shasum >/dev/null 2>&1; then
    hash="$(printf '%%s' "$material" | shasum -a 256 | cut -c1-16)"
  else
    return 0
  fi
  [ -n "$hash" ] || return 0
  # Only native PreToolUse enqueues: PermissionRequest also normalizes to
  # tool.requested but has no matching completion, so queueing it would leave
  # a stale id for the next completion to pop.
  if [ -n "$state_path" ] && [ "$event_type" = "tool.requested" ] &&
    [ "$(gram_hooks_native_event_name "$payload")" = "PreToolUse" ]; then
    printf 'hook_synth_%%s\n' "$hash" >>"$state_path" 2>/dev/null || true
    # Bound the queue so missed completions cannot grow it without limit.
    if [ "$(wc -l <"$state_path" 2>/dev/null)" -gt 32 ] 2>/dev/null; then
      tail -n 32 "$state_path" >"$state_path.tmp" 2>/dev/null && mv "$state_path.tmp" "$state_path" 2>/dev/null || rm -f "$state_path.tmp"
    fi
  fi
  printf 'hook_synth_%%s' "$hash"
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
  if [ -z "$skill" ] && [ "%s" = "codex" ]; then
    skill="$(gram_hooks_codex_skill_name "$payload" "$tool_name")"
  fi
  printf '%%s' "$skill"
}

# gram_hooks_codex_skill_exists validates a candidate skill name against the
# directories Codex discovers skills from: .agents/skills walking up from the
# session cwd, plus the user, admin, and Codex-home locations.
gram_hooks_codex_skill_exists() {
  local name="$1"
  local dir="$2"
  local root
  [ -n "$name" ] || return 1
  case "$name" in
    */* | .*) return 1 ;;
  esac
  [ -f "${HOME}/.agents/skills/${name}/SKILL.md" ] && return 0
  # Bundled/system skills live under a .system subdirectory of the skill
  # roots (e.g. ~/.codex/skills/.system/plan) but are mentioned by bare name.
  for root in /etc/codex/skills /opt/codex/skills "${CODEX_HOME:-${HOME}/.codex}/skills"; do
    [ -f "${root}/${name}/SKILL.md" ] && return 0
    [ -f "${root}/.system/${name}/SKILL.md" ] && return 0
  done
  while [ -n "$dir" ] && [ "$dir" != "/" ]; do
    [ -f "${dir}/.agents/skills/${name}/SKILL.md" ] && return 0
    # cwd comes from the provider payload: a value with no slash left would
    # make the strip a no-op and spin this loop forever.
    case "$dir" in
      */*) dir="${dir%%/*}" ;;
      *) dir="" ;;
    esac
  done
  return 1
}

# gram_hooks_codex_skill_name infers skill activations for Codex, which has no
# structured skill signal. Two best-effort detections:
#   - a reader tool opening a skills/<name>/SKILL.md path (implicit
#     activation: Codex is prompted to open SKILL.md when it picks a skill)
#   - an explicit $skill-name mention in the submitted prompt, accepted only
#     when the name resolves to a skill directory on disk (explicit mentions
#     are expanded internally and never surface as a tool call)
gram_hooks_codex_skill_name() {
  local payload="$1"
  local tool_name="$2"
  local native tool_input prompt cwd match token
  native="$(gram_hooks_native_event_name "$payload")"
  case "$native" in
    PreToolUse)
      case "$tool_name" in
        Bash | shell | Read) ;;
        *) return 0 ;;
      esac
      tool_input="$(gram_hooks_json_value "$payload" "tool_input")"
      [ -n "$tool_input" ] || return 0
      match="$(printf '%%s\n' "$tool_input" | sed -n 's/.*skills\/\(\.system\/\)\{0,1\}\([A-Za-z0-9][A-Za-z0-9._-]*\)\/SKILL\.md.*/\2/p' | sed -n '1p')"
      printf '%%s' "$match"
      ;;
    UserPromptSubmit)
      prompt="$(gram_hooks_json_string_value "$payload" "prompt")"
      [ -n "$prompt" ] || return 0
      case "$prompt" in
        *\$*) ;;
        *) return 0 ;;
      esac
      cwd="$(gram_hooks_json_string_value "$payload" "cwd")"
      for token in $(printf '%%s\n' "$prompt" | tr -c 'A-Za-z0-9._$-' '\n' | sed -n 's/^\$\([A-Za-z0-9][A-Za-z0-9._-]*\)$/\1/p' | sed 's/\.*$//' | sort -u); do
        if gram_hooks_codex_skill_exists "$token" "$cwd"; then
          printf '%%s' "$token"
          return 0
        fi
      done
      ;;
  esac
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
  tool_name="$(gram_hooks_json_string_value "$payload" "tool_name")"
  # tool_input.server names an MCP server only for the MCP meta-tools;
  # ordinary tools may take an unrelated "server" argument.
  case "$tool_name" in
    list_mcp_resources | list_mcp_resource_templates | read_mcp_resource | ListMcpResourcesTool | ReadMcpResourceTool)
      if command -v jq >/dev/null 2>&1; then
        server="$(printf '%%s' "$payload" | jq -r '(.tool_input.server // empty) | select(type == "string")' 2>/dev/null)" || true
        if [ -n "$server" ]; then
          printf '%%s' "$server"
          return 0
        fi
      fi
      ;;
  esac
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
      elif ($arg | test("^[A-Za-z_][A-Za-z0-9_]*(key|token|secret|password|passwd|credential|auth)[A-Za-z0-9_]*="; "i")) then
        {out: (.out + [($arg | sub("=.*"; "=***"))]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*="; "i")) then
        {out: (.out + [($arg | sub("=.*"; "=***"))]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*$"; "i")) then
        {out: (.out + [$arg]), next: true}
      elif ($arg | test("^(--?[^=]*=)?(authorization|proxy-authorization|cookie|x-api-key) *:"; "i")) then
        {out: (.out + [($arg | sub(":.*"; ": ***"))]), next: false}
      elif ($arg | test("^bearer$"; "i")) then
        {out: (.out + [$arg]), next: true}
      elif ($arg | test("bearer +[^ ]"; "i")) then
        {out: (.out + ["***"]), next: false}
      elif ($arg | test("://[^/@]*@|^(sk-|ghp_|gho_|github_pat_|xox[a-z]-|glpat-)")) then
        {out: (.out + ["***"]), next: false}
      else {out: (.out + [$arg]), next: false} end) | .out;
    (map(select(.name == $server)) | .[0]) as $exact |
    map(select((.name | sanitize) == $server)) as $fuzzy |
    ($exact // (if ($fuzzy | length) == 1 then $fuzzy[0] else null end)) as $m |
    if $m == null then
      empty
    else
      "url=\($m.transport.url // "")",
      "command=\(if $m.transport.type == "stdio" then ((([$m.transport.command] | clean) + (($m.transport.args // []) | clean | redact_args)) | join(" ")) else "" end)"
    end
  ' | head -n 2
}

# gram_hooks_redact_url strips credentials from an MCP server URL before it
# becomes telemetry or Shadow MCP evidence: basic-auth userinfo and fragments
# are dropped and secret-named query values are masked. Host, path and
# non-secret parameters survive so the evidence stays matchable.
gram_hooks_redact_url() {
  local url="$1"
  [ -n "$url" ] || return 0
  local fragmentless="${url%%%%#*}"
  local base="${fragmentless%%%%\?*}"
  local query=""
  case "$fragmentless" in
    *\?*) query="${fragmentless#*\?}" ;;
  esac
  case "$base" in
    *://*@*)
      local scheme="${base%%%%://*}" rest="${base#*://}" authority path=""
      authority="${rest%%%%/*}"
      case "$rest" in
        */*) path="/${rest#*/}" ;;
      esac
      authority="${authority##*@}"
      base="${scheme}://${authority}${path}"
      ;;
  esac
  if [ -z "$query" ]; then
    printf '%%s' "$base"
    return 0
  fi
  local out="" pair key lower
  while IFS= read -r pair; do
    [ -n "$pair" ] || continue
    case "$pair" in
      *=*)
        key="${pair%%%%=*}"
        lower="$(printf '%%s' "$key" | tr '[:upper:]' '[:lower:]')"
        case "$lower" in
          *key* | *token* | *secret* | *password* | *passwd* | *credential* | *auth*)
            pair="${key}=***"
            ;;
        esac
        ;;
    esac
    if [ -n "$out" ]; then
      out="${out}&${pair}"
    else
      out="$pair"
    fi
  done <<GRAM_HOOKS_QUERY
$(printf '%%s\n' "$query" | tr '&' '\n')
GRAM_HOOKS_QUERY
  printf '%%s?%%s' "$base" "$out"
}

# gram_hooks_scrub_raw_payload rewrites the secret-bearing top-level keys of
# the provider payload before it is echoed under "raw" (a debugging aid the
# backend never reads): url/mcp_server_url/command can carry credentials, and
# redacting data.mcp alone would leave the same values in the echo. Without
# jq the payload cannot be rewritten, so raw is dropped instead of leaking.
gram_hooks_scrub_raw_payload() {
  local payload="$1"
  case "$payload" in
    *\"url\"* | *\"mcp_server_url\"* | *\"command\"*) ;;
    *)
      printf '%%s' "$payload"
      return 0
      ;;
  esac
  if command -v jq >/dev/null 2>&1; then
    local url mcp_url command
    url="$(gram_hooks_redact_url "$(gram_hooks_json_string_value "$payload" "url")")"
    mcp_url="$(gram_hooks_redact_url "$(gram_hooks_json_string_value "$payload" "mcp_server_url")")"
    command="$(gram_hooks_redact_command_string "$(gram_hooks_json_string_value "$payload" "command")")"
    printf '%%s' "$payload" | jq -c --arg url "$url" --arg mcp_url "$mcp_url" --arg command "$command" '
      if type == "object" then
        (if has("url") then .url = $url else . end) |
        (if has("mcp_server_url") then .mcp_server_url = $mcp_url else . end) |
        (if has("command") then .command = $command else . end)
      else . end' 2>/dev/null && return 0
  fi
  printf 'null'
}

# gram_hooks_redact_command_string applies the same argument redaction as the
# config-discovery paths to a provider-supplied command line, so credentials
# passed as stdio server arguments never reach telemetry or block evidence.
# Without jq nothing is returned: a truncated command (e.g. bare "npx") would
# collapse distinct servers into one Shadow MCP identity, so identity falls
# back to the server alias instead. Tokenization splits on spaces and cannot
# see through shell quoting; the patterns cover the common unquoted shapes.
gram_hooks_redact_command_string() {
  local command="$1"
  [ -n "$command" ] || return 0
  command -v jq >/dev/null 2>&1 || return 0
  # Quote characters hide flags from the token patterns; strip them so
  # quoted arguments still match. Values are redacted, never re-executed.
  command="${command//\"/}"
  command="${command//\'/}"
  printf '%%s' "$command" | jq -Rr '
    def redact_args: reduce .[] as $arg ({out: [], next: false};
      if .next then {out: (.out + ["***"]), next: false}
      elif ($arg | test("^[A-Za-z_][A-Za-z0-9_]*(key|token|secret|password|passwd|credential|auth)[A-Za-z0-9_]*="; "i")) then
        {out: (.out + [($arg | sub("=.*"; "=***"))]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*="; "i")) then
        {out: (.out + [($arg | sub("=.*"; "=***"))]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*$"; "i")) then
        {out: (.out + [$arg]), next: true}
      elif ($arg | test("^(--?[^=]*=)?(authorization|proxy-authorization|cookie|x-api-key) *:"; "i")) then
        {out: (.out + [($arg | sub(":.*"; ": ***"))]), next: false}
      elif ($arg | test("^bearer$"; "i")) then
        {out: (.out + [$arg]), next: true}
      elif ($arg | test("bearer +[^ ]"; "i")) then
        {out: (.out + ["***"]), next: false}
      elif ($arg | test("://[^/@]*@|^(sk-|ghp_|gho_|github_pat_|xox[a-z]-|glpat-)")) then
        {out: (.out + ["***"]), next: false}
      else {out: (.out + [$arg]), next: false} end) | .out;
    (split(" ") | map(select(. != ""))) as $t |
    if ($t | length) == 0 then "" else ($t | redact_args | join(" ")) end
    ' 2>/dev/null
}

# Claude's mcp__<server>__<tool> prefixes carry a sanitized form of the config
# display name, so the lookup falls back to comparing sanitized keys. The jq
# sanitize def below must stay in lockstep with
# gram_hooks_sanitize_claude_mcp_name in the Claude enrichment snippet.
gram_hooks_mcp_metadata_from_file() {
  local file="$1"
  local server="$2"
  [ -f "$file" ] || return 0
  [ -n "$server" ] || return 0
  command -v jq >/dev/null 2>&1 || return 0
  jq -r --arg server "$server" '
    def clean: map(select(. != null and . != "") | tostring);
    def sanitize: gsub(" "; "_") | gsub("[()]"; "") | gsub("_{2,}"; "_") | sub("^_+"; "") | sub("_+$"; "");
    def redact_args: reduce .[] as $arg ({out: [], next: false};
      if .next then {out: (.out + ["***"]), next: false}
      elif ($arg | test("^[A-Za-z_][A-Za-z0-9_]*(key|token|secret|password|passwd|credential|auth)[A-Za-z0-9_]*="; "i")) then
        {out: (.out + [($arg | sub("=.*"; "=***"))]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*="; "i")) then
        {out: (.out + [($arg | sub("=.*"; "=***"))]), next: false}
      elif ($arg | test("^--?[^=]*(key|token|secret|password|passwd|credential|bearer|auth)[^=]*$"; "i")) then
        {out: (.out + [$arg]), next: true}
      elif ($arg | test("^(--?[^=]*=)?(authorization|proxy-authorization|cookie|x-api-key) *:"; "i")) then
        {out: (.out + [($arg | sub(":.*"; ": ***"))]), next: false}
      elif ($arg | test("^bearer$"; "i")) then
        {out: (.out + [$arg]), next: true}
      elif ($arg | test("bearer +[^ ]"; "i")) then
        {out: (.out + ["***"]), next: false}
      elif ($arg | test("://[^/@]*@|^(sk-|ghp_|gho_|github_pat_|xox[a-z]-|glpat-)")) then
        {out: (.out + ["***"]), next: false}
      else {out: (.out + [$arg]), next: false} end) | .out;
    (.mcpServers // {}) as $servers |
    ($servers[$server] // (
      $servers | to_entries | map(select((.key | sanitize) == $server)) |
      if length == 1 then .[0].value else null end
    )) as $m |
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
  printf '%%s\n' "$(gram_hooks_cursor_prompt_fingerprint "$(gram_hooks_json_string_value "$payload" "prompt")")" >"$state_path" 2>/dev/null || true
}

# gram_hooks_cursor_deny_scope reduces a payload's generation id to a single
# stash-line token; "-" when the payload carries none.
gram_hooks_cursor_deny_scope() {
  local scope
  scope="$(gram_hooks_json_string_value "$1" "generation_id" | tr -c 'A-Za-z0-9_.-' '_')"
  [ -n "$scope" ] || scope="-"
  printf '%%s' "$scope"
}

# gram_hooks_cursor_take_pending_prompt_deny prints the deny body stashed by
# a backfill that ran on a non-decision event, consuming it so the deny is
# relayed exactly once. The stash is scoped to the generation it was denied
# for: when the denied turn ends without a decision event, the next turn must
# not inherit the block — its own backfill re-checks its own prompt — so a
# stash from another generation is discarded instead of printed. Returns 1
# when nothing is pending for this generation.
gram_hooks_cursor_take_pending_prompt_deny() {
  local payload="$1"
  local server_url_arg="$2"
  local project_slug_arg="$3"
  local session_id state_path pending stashed_scope
  session_id="$(gram_hooks_session_id "$payload")"
  state_path="$(gram_hooks_cursor_prompt_state_path "$session_id" "$server_url_arg" "$project_slug_arg")" || return 1
  pending="$(sed -n '2p' "$state_path" 2>/dev/null)"
  case "$pending" in
    "deny "*) ;;
    *) return 1 ;;
  esac
  sed -n '1p' "$state_path" >"$state_path.tmp" 2>/dev/null && mv "$state_path.tmp" "$state_path" 2>/dev/null || rm -f "$state_path.tmp"
  pending="${pending#deny }"
  stashed_scope="${pending%%%% *}"
  if [ "$stashed_scope" != "$(gram_hooks_cursor_deny_scope "$payload")" ]; then
    return 1
  fi
  printf '%%s' "${pending#* }"
}

# gram_hooks_cursor_prompt_fingerprint reduces a prompt to a stable content
# fingerprint. Trailing blank lines are stripped first so the payload prompt
# and its transcript rendering normalize identically.
gram_hooks_cursor_prompt_fingerprint() {
  printf '%%s' "$1" | sed -e :a -e '/^[[:space:]]*$/{$d;N;ba' -e '}' | cksum 2>/dev/null | tr -s ' \t' '_'
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

  # Take the LATEST user entry: during turn N the transcript already holds
  # prompts 1..N, and the backfill targets the current turn's prompt.
  encoded="$(jq -r '
    select(.role == "user")
    | [.message.content[]? | select(.type == "text") | .text]
    | join("\n")
    | @base64
  ' "$transcript_path" 2>/dev/null | sed -n '$p')" || true
  [ -n "$encoded" ] || return 0
  printf '%%s' "$encoded" | gram_hooks_base64_decode 2>/dev/null | gram_hooks_cursor_clean_transcript_prompt
}

gram_hooks_cursor_backfill_prompt_if_missing() {
  local payload="$1"
  local hostname="$2"
  local server_url_arg="$3"
  local project_slug_arg="$4"
  local session_id state_path prompt fingerprint prompt_payload prompt_members canonical_prompt http_code

  session_id="$(gram_hooks_session_id "$payload")"
  state_path="$(gram_hooks_cursor_prompt_state_path "$session_id" "$server_url_arg" "$project_slug_arg")" || return 0

  prompt="$(gram_hooks_cursor_transcript_prompt "$payload")"
  [ -n "$prompt" ] || return 0

  # The marker stores a fingerprint of the last prompt handled (delivered or
  # backfilled), not a per-session boolean, so a beforeSubmitPrompt dropped on
  # a LATER turn is still backfilled. Consecutive identical prompts are
  # indistinguishable from already-handled ones and are not re-sent.
  fingerprint="$(gram_hooks_cursor_prompt_fingerprint "$prompt")"
  if [ -r "$state_path" ] && [ "$(sed -n '1p' "$state_path" 2>/dev/null)" = "$fingerprint" ]; then
    return 0
  fi

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
  gram_hooks_post_authenticated "$server_url_arg" "$canonical_prompt" 10 "$project_slug_arg" "${gram_hooks_failure_exit:-2}"
  unset GRAM_IDEMPOTENCY_TOKEN
  http_code="$GRAM_HTTP_CODE"
  if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
    # Surface the server's verdict on the backfilled prompt: a deny would have
    # fired at beforeSubmitPrompt had that delivery not been missed, so the
    # caller can still relay it on the current decision event.
    GRAM_HOOKS_BACKFILL_STATUS="ok"
    GRAM_HOOKS_BACKFILL_DECISION="$(gram_hooks_json_string_value "$GRAM_HTTP_BODY" "decision")"
    GRAM_HOOKS_BACKFILL_BODY="$GRAM_HTTP_BODY"
    if [ "$GRAM_HOOKS_BACKFILL_DECISION" = "deny" ]; then
      # This invocation may be a non-decision event (stop, afterAgentResponse)
      # that cannot relay the deny; stash it with the marker so the turn's
      # next decision event relays it instead of the deny being swallowed.
      # The stash carries the generation id so it can never block a later
      # unrelated turn.
      printf '%%s\ndeny %%s %%s\n' "$fingerprint" "$(gram_hooks_cursor_deny_scope "$payload")" "$GRAM_HOOKS_BACKFILL_BODY" >"$state_path" 2>/dev/null || true
    else
      printf '%%s\n' "$fingerprint" >"$state_path" 2>/dev/null || true
    fi
  elif [ -n "$http_code" ]; then
    # A real attempt failed: this backfill was the turn's only prompt-policy
    # check, so decision events must not proceed on an unverified prompt.
    # An empty code is the never-authenticated pass-through and keeps the
    # ratchet's fail-open behavior.
    GRAM_HOOKS_BACKFILL_STATUS="failed"
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
  # Codex skill names are inferred from ordinary tool/prompt payloads, so the
  # wire event keeps its true type there: reclassifying would skip the
  # server's tool/prompt policy scan for any payload that happens to mention
  # a SKILL.md path. The skill lands in data.skill and the server layers the
  # skill.activated classification on top.
  if [ "%s" != "codex" ] && [ -n "$(gram_hooks_skill_name "$payload")" ]; then
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
  raw_json="$(gram_hooks_json_raw_or_string "$(gram_hooks_scrub_raw_payload "$payload")")"

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
    if [ -z "$tool_id" ]; then
      tool_id="$(gram_hooks_synth_tool_id "$payload" "$tool_name" "$tool_input" "$event_type")"
    fi
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
      if [ -n "$tool_members" ]; then
        tool_members="${tool_members},\"is_interrupt\":$is_interrupt"
      else
        tool_members="\"is_interrupt\":$is_interrupt"
      fi
    fi
    tool_members="\"tool_call\":{$tool_members}"
  else
    tool_members=""
  fi

  local mcp_server_name mcp_server_identity mcp_url mcp_command mcp_metadata
  mcp_server_name="$(gram_hooks_mcp_server_from_payload "$payload")"
  mcp_server_identity="$(gram_hooks_json_string_value "$payload" "server_identity")"
  mcp_url="$(gram_hooks_first_string "$payload" "url" "mcp_server_url")"
  # Provider-supplied commands carry whatever argv the server was launched
  # with; redact like the config-discovery paths before this string becomes
  # telemetry or the Shadow MCP identity.
  mcp_command="$(gram_hooks_redact_command_string "$(gram_hooks_json_string_value "$payload" "command")")"
  if [ -n "$mcp_server_name" ] && { [ -z "$mcp_url" ] || [ -z "$mcp_command" ]; }; then
    mcp_metadata="$(gram_hooks_local_mcp_metadata "$mcp_server_name")"
    [ -n "$mcp_metadata" ] || mcp_metadata="$(gram_hooks_codex_mcp_metadata "$mcp_server_name")"
    if [ -n "$mcp_metadata" ]; then
      [ -n "$mcp_url" ] || mcp_url="$(printf '%%s\n' "$mcp_metadata" | sed -n 's/^url=//p')"
      [ -n "$mcp_command" ] || mcp_command="$(printf '%%s\n' "$mcp_metadata" | sed -n 's/^command=//p')"
    fi
  fi
  # URLs from provider payloads and config lookups can embed credentials
  # (basic-auth userinfo, secret query params); strip them here so every
  # downstream use — telemetry, identity, block evidence — sees the redacted
  # form only.
  mcp_url="$(gram_hooks_redact_url "$mcp_url")"
  # Identity is what Shadow MCP approvals are scoped to. Pin stdio servers to
  # their launch command — the server alias is mutable and must not inherit an
  # approval after being repointed at a different command. URL servers carry
  # their URL as separate evidence, so the alias suffices there.
  if [ -z "$mcp_server_identity" ]; then
    if [ -z "$mcp_url" ] && [ -n "$mcp_command" ]; then
      mcp_server_identity="$mcp_command"
    else
      mcp_server_identity="$mcp_server_name"
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
      elif [ "$native" = "SessionStart" ]; then
        printf '{"continue":true}'
      else
        printf '{}'
      fi
      ;;
    cursor)
      if [ "$decision" = "deny" ]; then
        printf '{"permission":"deny","user_message":"%%s","agent_message":"%%s"}' "$escaped" "$escaped"
      else
        printf '{}'
      fi
      ;;
    *)
      printf '%%s' "$body"
      ;;
  esac
}
`, spec.Platform, cases.String(), spec.Platform, spec.Platform, spec.Platform, spec.Platform, spec.Platform)
}
