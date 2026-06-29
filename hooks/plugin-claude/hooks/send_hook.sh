#!/usr/bin/env bash

# Send a Claude Code hook event to Gram. The server is the sole authority on
# whether to block:
#   HTTP 2xx -> allow (exit 0). The JSON body is forwarded to Claude as-is;
#               for PreToolUse, Claude reads `hookSpecificOutput.permissionDecision`
#               from that body to honor any deny decision the server made.
#   HTTP 4xx/5xx -> block (exit 2). The server's `message` is relayed to
#                   stderr so Claude renders it as the block reason.
# We do not parse the body to derive the exit code — the script never makes
# the allow/deny decision, only the server does.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$script_dir/identity.sh" ]; then
  . "$script_dir/identity.sh"
fi
. "$script_dir/http.sh"

payload=$(cat)
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi

# Stop-collection protocol version. Advertise it only when jq is available to
# parse and normalize the transcript; otherwise the server must keep the legacy
# per-event PG persistence path enabled. Must match
# claudeHookStopCollectionVersion on the server.
batch_capture_available=false
hook_version_header=()
if command -v jq >/dev/null 2>&1; then
  batch_capture_available=true
  hook_version_header=(-H "X-Gram-Hook-Version: 2")
fi
auth_config=""
auth_config_arg=()
cleanup_auth_config() {
  if [ -n "$auth_config" ]; then
    rm -f "$auth_config"
  fi
}
trap cleanup_auth_config EXIT
if [ -n "${GRAM_HOOKS_API_KEY:-}" ] || [ -n "${GRAM_HOOKS_PROJECT_SLUG:-}" ]; then
  if ! auth_config=$(mktemp "${TMPDIR:-/tmp}/gram-hooks-curl.XXXXXX"); then
    # Fail closed (exit 2) like every other failure path, but say why —
    # otherwise Claude shows a blocked tool call with an empty reason.
    echo "Speakeasy hooks: could not create a temporary auth file on this machine, so the tool call was blocked. Check that ${TMPDIR:-/tmp} is writable." >&2
    exit 2
  fi
  chmod 600 "$auth_config" || true
  if [ -n "${GRAM_HOOKS_API_KEY:-}" ]; then
    printf 'header = "Gram-Key: %s"\n' "$GRAM_HOOKS_API_KEY" >>"$auth_config"
  fi
  if [ -n "${GRAM_HOOKS_PROJECT_SLUG:-}" ]; then
    printf 'header = "Gram-Project: %s"\n' "$GRAM_HOOKS_PROJECT_SLUG" >>"$auth_config"
  fi
  auth_config_arg=(--config "$auth_config")
fi

# transcript_cursor_file echoes the path of the per-transcript cursor checkpoint
# for this event, or nothing when the transcript path is unknown. Keyed on a
# checksum of the transcript path so the parent transcript and each subagent
# transcript get an independent cursor.
transcript_cursor_file() {
  local hook_payload="$1" hook_event="$2" tpath sid key
  if [ "$hook_event" = "SubagentStop" ]; then
    tpath=$(printf '%s' "$hook_payload" | jq -r '.agent_transcript_path // empty' 2>/dev/null || true)
  else
    tpath=$(printf '%s' "$hook_payload" | jq -r '.transcript_path // empty' 2>/dev/null || true)
  fi
  [ -z "$tpath" ] && return 0
  sid=$(printf '%s' "$hook_payload" | jq -r '.session_id // empty' 2>/dev/null || true)
  sid=$(printf '%s' "$sid" | tr -cd 'A-Za-z0-9_-' | cut -c1-128)
  key=$(printf '%s' "$tpath" | cksum | awk '{print $1}')
  printf '%s/gram-hooks/cursor-%s-%s' "${TMPDIR:-/tmp}" "$sid" "$key"
}

build_capture_body() {
  local hook_payload="$1"
  local hook_event="$2"
  local cursor="${3:-}"
  local transcript_path session_id user_email agent_id agent_type tools_only

  session_id=$(printf '%s' "$hook_payload" | jq -r '.session_id // empty' 2>/dev/null || true)
  if [ -z "$session_id" ]; then
    return 1
  fi

  if [ "$hook_event" = "SubagentStop" ]; then
    transcript_path=$(printf '%s' "$hook_payload" | jq -r '.agent_transcript_path // empty' 2>/dev/null || true)
    agent_id=$(printf '%s' "$hook_payload" | jq -r '.agent_id // empty' 2>/dev/null || true)
    agent_type=$(printf '%s' "$hook_payload" | jq -r '.agent_type // empty' 2>/dev/null || true)
    tools_only=true
  else
    transcript_path=$(printf '%s' "$hook_payload" | jq -r '.transcript_path // empty' 2>/dev/null || true)
    agent_id=""
    agent_type=""
    tools_only=false
  fi
  user_email=$(printf '%s' "$hook_payload" | jq -r '.user_email // empty' 2>/dev/null || true)

  if [ -z "$transcript_path" ] || [ ! -r "$transcript_path" ]; then
    return 1
  fi

  # Parse each JSONL line independently first (fromjson? drops unparseable
  # lines) so a partial trailing line — common when Stop fires while the
  # transcript is still flushing — can't abort the whole slurp and empty the
  # batch.
  jq -R 'fromjson? // empty' "$transcript_path" 2>/dev/null \
  | jq -c -s \
    --arg session_id "$session_id" \
    --arg user_email "$user_email" \
    --arg agent_id "$agent_id" \
    --arg agent_type "$agent_type" \
    --arg cursor "$cursor" \
    --argjson tools_only "$tools_only" '
def command_wrapper:
  test("^\\s*<(command-name|command-message|command-args|local-command-stdout|local-command-caveat)\\b");

def content_string($v):
  if ($v | type) == "string" then $v
  elif $v == null then ""
  else ($v | tojson)
  end;

def maybe_ts($ts):
  if $ts == "" then . else . + {timestamp: $ts} end;

def maybe_agent($agent_id; $agent_type):
  . + (if $agent_id == "" then {} else {agent_id: $agent_id} end)
    + (if $agent_type == "" then {} else {agent_type: $agent_type} end);

def maybe_model($model):
  if $model == "" then . else . + {model: $model} end;

def number_or_zero($v):
  try ($v // 0 | tonumber) catch 0;

def usage_fields($message):
  # prompt_tokens folds in cache read/creation so the total matches the
  # OpenRouter proxy path, whose prompt_tokens already includes cached input.
  # Claude transcripts report them separately, so input_tokens alone
  # undercounts cached turns badly.
  (number_or_zero($message.usage.input_tokens)
    + number_or_zero($message.usage.cache_read_input_tokens)
    + number_or_zero($message.usage.cache_creation_input_tokens)) as $pt
  | (number_or_zero($message.usage.output_tokens)) as $ct
  | {}
    + (if $pt == 0 then {} else {prompt_tokens: $pt} end)
    + (if $ct == 0 then {} else {completion_tokens: $ct} end)
    + (if ($pt + $ct) == 0 then {} else {total_tokens: ($pt + $ct)} end);

def base_msg($external_id; $role; $ts; $agent_id; $agent_type; $content):
  {external_id: $external_id, role: $role, content: $content}
  | maybe_ts($ts)
  | maybe_agent($agent_id; $agent_type);

def tool_calls($blocks):
  [
    $blocks[]?
    | select(.type == "tool_use")
    | {
        id: (.id // ""),
        type: "function",
        function: {
          name: (.name // ""),
          arguments: ((.input // {}) | tojson)
        }
      }
  ];

def emit($tools_only; $agent_id; $agent_type):
  . as $entry
  | ($entry.uuid // "") as $uid
  | select($uid != "")
  | ($entry.type // "") as $entry_type
  | ($entry.timestamp // "") as $ts
  | if $entry_type == "user" then
      if (($entry.message.content? | type) == "array") then
        (
          $entry.message.content[]?
          | select(.type == "tool_result")
          | (.tool_use_id // "") as $tool_call_id
          | select($tool_call_id != "")
          | base_msg($tool_call_id; "tool"; $ts; $agent_id; $agent_type; content_string(.content))
            + {tool_call_id: $tool_call_id}
        ),
        (
          # Text blocks in an array-form user turn (e.g. a prompt pasted with an
          # image/file) would otherwise be dropped. Emit them as one user
          # message keyed on the entry uuid, unless this is a tools-only
          # subagent transcript, a meta entry, or a slash-command wrapper.
          select(($tools_only | not) and (($entry.isMeta // false) != true))
          | [ $entry.message.content[]? | select(.type == "text" and (.text // "") != "") | .text ] as $texts
          | select(($texts | length) > 0)
          | ($texts | join("\n")) as $content
          | select(($content | command_wrapper) | not)
          | base_msg($uid; "user"; $ts; $agent_id; $agent_type; $content)
        )
      elif $tools_only then
        empty
      else
        ($entry.message.content? // "") as $content
        | select(($content | type) == "string")
        | select(($entry.isMeta // false) != true)
        | select(($content | command_wrapper) | not)
        | base_msg($uid; "user"; $ts; $agent_id; $agent_type; $content)
      end
    elif $entry_type == "assistant" then
      ($entry.message.content? // []) as $blocks
      | select(($blocks | type) == "array")
      | [ $blocks[]? | select(.type == "text" and (.text // "") != "") | .text ] as $texts
      | tool_calls($blocks) as $tools
      | ($entry.message.model // "") as $model
      | if ($tools | length) > 0 then
          base_msg($uid; "assistant"; $ts; $agent_id; $agent_type; ($texts | join(" ")))
          | maybe_model($model)
          | . + {tool_calls: $tools}
          | . + usage_fields($entry.message)
        elif (($texts | length) > 0 and ($tools_only | not)) then
          base_msg($uid; "assistant"; $ts; $agent_id; $agent_type; ($texts | join(" ")))
          | maybe_model($model)
          | . + usage_fields($entry.message)
        else
          empty
        end
    elif ($entry_type == "system" and ($tools_only | not)) then
      ($entry.message.content? // $entry.content? // "") as $content
      | select(($content | type) == "string" and $content != "")
      | base_msg($uid; "system"; $ts; $agent_id; $agent_type; $content)
    else
      empty
    end;

([.[] | .uuid // ""]) as $uids
| ([.[] | .uuid // empty] | last) as $new_cursor
| (if $cursor == "" then 0 else (($uids | index($cursor)) // -1) + 1 end) as $start
| [.[$start:][] | emit($tools_only; $agent_id; $agent_type)] as $messages
| {
    cursor: ($new_cursor // ""),
    body: (if ($messages | length) > 0
           then {session_id: $session_id, messages: $messages}
                + (if $user_email == "" then {} else {user_email: $user_email} end)
           else null end)
  }
' 2>/dev/null
}

# Stop and SubagentStop carry the completed transcript. Conversation capture is
# idempotent server-side (deduped by external_id), so these route to the batch
# capture endpoint built from the transcript file rather than the per-event
# path when jq is available. A per-transcript cursor (the last sent entry uuid)
# means each Stop only sends messages appended since the previous Stop instead
# of resending the whole transcript every turn; the cursor only advances after a
# 2xx, and a stale/missing cursor just resends (the server dedupes by
# external_id). If Stop extraction or delivery fails, fall back to the legacy
# per-event endpoint without the v2 header. SubagentStop has no legacy endpoint
# representation, so failed batch capture is best-effort.
hook_event=""
if command -v jq >/dev/null 2>&1; then
  hook_event=$(printf '%s' "$payload" | jq -r '.hook_event_name // empty' 2>/dev/null || true)
fi
if [ -z "$hook_event" ]; then
  hook_event=$(printf '%s' "$payload" | sed -n 's/.*"hook_event_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
fi

if [ "$hook_event" = "Stop" ] || [ "$hook_event" = "SubagentStop" ]; then
  if [ "$batch_capture_available" = true ]; then
    cursor_file=$(transcript_cursor_file "$payload" "$hook_event")
    old_cursor=""
    if [ -n "$cursor_file" ] && [ -r "$cursor_file" ]; then
      old_cursor=$(cat "$cursor_file" 2>/dev/null || true)
    fi
    capture_result=$(build_capture_body "$payload" "$hook_event" "$old_cursor" || true)
    if [ -n "$capture_result" ]; then
      # build succeeded (a non-empty result is always a {cursor, body} object).
      capture_body=$(printf '%s' "$capture_result" | jq -c 'select(.body != null) | .body' 2>/dev/null || true)
      new_cursor=$(printf '%s' "$capture_result" | jq -r '.cursor // empty' 2>/dev/null || true)
      if [ -n "$capture_body" ]; then
        gram_http_post "${server_url}/rpc/hooks.claudeMessages" "$capture_body" 10 \
          ${hook_hostname_header[@]+"${hook_hostname_header[@]}"} \
          ${hook_version_header[@]+"${hook_version_header[@]}"} \
          ${auth_config_arg[@]+"${auth_config_arg[@]}"}
        http_code="$GRAM_HTTP_CODE"
        if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
          if [ -n "$cursor_file" ] && [ -n "$new_cursor" ]; then
            mkdir -p "$(dirname "$cursor_file")" 2>/dev/null || true
            printf '%s' "$new_cursor" >"$cursor_file" 2>/dev/null || true
          fi
          exit 0
        fi
        # Delivery failed: leave the cursor unadvanced so the next Stop retries
        # these messages, and fall through to the legacy fallback below.
      else
        # Nothing new since the last cursor — already captured, nothing to send.
        exit 0
      fi
    fi
    if [ "$hook_event" = "SubagentStop" ]; then
      exit 0
    fi
    hook_version_header=()
  fi
fi

# Retries transient resets (see http.sh) so a single reset no longer blocks
# the tool call; the server still decides allow/block from the HTTP code.
gram_http_post "${server_url}/rpc/hooks.claude" "$payload" 10 \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"} \
  ${hook_version_header[@]+"${hook_version_header[@]}"} \
  ${auth_config_arg[@]+"${auth_config_arg[@]}"}

http_code="$GRAM_HTTP_CODE"
body="$GRAM_HTTP_BODY"

# Forward the body to stdout so Claude can read PreToolUse decisions from it.
echo "$body"

# Only treat a real 2xx as allow. curl returns 000 on connection failure, DNS
# error, or timeout, and a 3xx (e.g. an http->https redirect, which curl does
# not follow here) carries no decision body — neither must silently allow the
# call, otherwise blocking policies are bypassed when the server is unreachable
# or misconfigured. The 2>/dev/null guards keep a non-numeric code from leaking
# a shell error before we fall through to the block path below.
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
  exit 0
fi

# Best-effort: extract the server's `message` (already self-branded as
# "Speakeasy blocked this prompt: ...") so Claude shows it to the user. Try jq
# then python3 so the reason still renders on machines that have only one of
# them; falls back to a generic line if neither is available or the body isn't
# parseable, so the script still blocks correctly on minimal systems.
reason=""
if command -v jq >/dev/null 2>&1; then
  reason=$(printf '%s' "$body" | jq -r '.message // empty' 2>/dev/null || true)
fi
if [ -z "$reason" ] && command -v python3 >/dev/null 2>&1; then
  reason=$(printf '%s' "$body" | python3 -c "
import json, sys
try:
    print(json.loads(sys.stdin.read()).get('message', ''), end='')
except Exception:
    pass
" 2>/dev/null) || true
fi

echo "${reason:-Speakeasy hook returned HTTP ${http_code}}" >&2
exit 2
