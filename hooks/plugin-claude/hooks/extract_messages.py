#!/usr/bin/env python3
# Build the /rpc/hooks.claudeMessages request body from a Claude Code transcript.
#
# Reads the raw hook payload (Stop or SubagentStop) on stdin and prints a JSON
# body of normalized, dedup-keyed messages to stdout. Each message carries its
# transcript line UUID as `external_id` — the server's per-chat dedup key — so
# re-delivery from multiple plugin installations stores each message once.
#
#   Stop          -> read `transcript_path` (main agent): user prompts +
#                    assistant text (incl. mid-turn) + tool calls + tool results.
#   SubagentStop  -> read `agent_transcript_path` (subagent): tool calls + tool
#                    results ONLY, tagged with agent_id/agent_type, attributed to
#                    the same (parent) session — parity with today's capture.
#
# Lines without a uuid (sidecar state), isMeta/command-wrapper user lines, and
# assistant thinking blocks are dropped. Prints nothing when there is nothing to
# send, so the caller can skip the POST.
import sys
import json
import os
import re

_COMMAND_WRAPPER = re.compile(
    r"^\s*<(command-name|command-message|command-args|local-command-stdout|local-command-caveat)\b"
)


def _carr(entry):
    content = (entry.get("message") or {}).get("content")
    return content if isinstance(content, list) else None


def _cstr(entry):
    content = (entry.get("message") or {}).get("content")
    return content if isinstance(content, str) else None


def _read_lines(path):
    out = []
    if not path or not os.path.exists(path):
        return out
    with open(path) as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except Exception:
                pass
    return out


def _tool_calls(blocks):
    calls = []
    for b in blocks:
        if b.get("type") == "tool_use":
            calls.append(
                {
                    "id": b.get("id") or "",
                    "type": "function",
                    "function": {
                        "name": b.get("name") or "",
                        "arguments": json.dumps(
                            b.get("input") if b.get("input") is not None else {}
                        ),
                    },
                }
            )
    return calls


def _usage_tokens(message):
    usage = message.get("usage") or {}
    prompt = int(usage.get("input_tokens") or 0)
    completion = int(usage.get("output_tokens") or 0)
    return prompt, completion, prompt + completion


def _emit(entry, tools_only, agent_id, agent_type):
    """Yield normalized message dict(s) for a single transcript entry, or None."""
    uid = entry.get("uuid")
    if not uid:
        return []
    t = entry.get("type")
    ts = entry.get("timestamp")
    out = []

    if t == "user":
        # tool results live in an array-content user line
        arr = _carr(entry)
        if arr is not None:
            for b in arr:
                if b.get("type") == "tool_result":
                    content = b.get("content")
                    out.append(
                        _msg(
                            uid,
                            "tool",
                            ts,
                            agent_id,
                            agent_type,
                            content=content
                            if isinstance(content, str)
                            else json.dumps(content),
                            tool_call_id=b.get("tool_use_id") or "",
                        )
                    )
            return out
        if tools_only:
            return out
        s = _cstr(entry)
        if s is None or entry.get("isMeta") or _COMMAND_WRAPPER.match(s):
            return out
        out.append(_msg(uid, "user", ts, agent_id, agent_type, content=s))
        return out

    if t == "assistant":
        arr = _carr(entry)
        if arr is None:
            return out
        texts = [
            b.get("text") for b in arr if b.get("type") == "text" and b.get("text")
        ]
        tools = _tool_calls(arr)
        message = entry.get("message") or {}
        model = message.get("model") or ""
        pt, ct, tt = _usage_tokens(message)
        if tools:
            out.append(
                _msg(
                    uid,
                    "assistant",
                    ts,
                    agent_id,
                    agent_type,
                    content=" ".join(texts),
                    model=model,
                    tool_calls=tools,
                    prompt_tokens=pt,
                    completion_tokens=ct,
                    total_tokens=tt,
                )
            )
        elif texts and not tools_only:
            out.append(
                _msg(
                    uid,
                    "assistant",
                    ts,
                    agent_id,
                    agent_type,
                    content=" ".join(texts),
                    model=model,
                    prompt_tokens=pt,
                    completion_tokens=ct,
                    total_tokens=tt,
                )
            )
        # thinking-only / empty -> dropped
        return out

    return out


def _msg(
    external_id,
    role,
    ts,
    agent_id,
    agent_type,
    content="",
    model=None,
    tool_calls=None,
    tool_call_id=None,
    prompt_tokens=0,
    completion_tokens=0,
    total_tokens=0,
):
    m = {"external_id": external_id, "role": role, "content": content}
    if model:
        m["model"] = model
    if tool_calls:
        m["tool_calls"] = tool_calls
    if tool_call_id:
        m["tool_call_id"] = tool_call_id
    if prompt_tokens:
        m["prompt_tokens"] = prompt_tokens
    if completion_tokens:
        m["completion_tokens"] = completion_tokens
    if total_tokens:
        m["total_tokens"] = total_tokens
    if ts:
        m["timestamp"] = ts
    if agent_id:
        m["agent_id"] = agent_id
    if agent_type:
        m["agent_type"] = agent_type
    return m


def main():
    try:
        payload = json.loads(sys.stdin.read())
    except Exception:
        return
    if not isinstance(payload, dict):
        return

    event = payload.get("hook_event_name")
    session_id = payload.get("session_id")
    if not session_id:
        return

    if event == "SubagentStop":
        path = payload.get("agent_transcript_path")
        tools_only = True
        agent_id = payload.get("agent_id") or ""
        agent_type = payload.get("agent_type") or ""
    elif event == "Stop":
        path = payload.get("transcript_path")
        tools_only = False
        agent_id = ""
        agent_type = ""
    else:
        return

    messages = []
    for entry in _read_lines(path):
        messages.extend(_emit(entry, tools_only, agent_id, agent_type))

    if not messages:
        return

    body = {"session_id": session_id, "messages": messages}
    user_email = payload.get("user_email")
    if user_email:
        body["user_email"] = user_email
    sys.stdout.write(json.dumps(body))


if __name__ == "__main__":
    main()
