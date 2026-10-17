# Hooks Service

The hooks service supports three hook entry points:

- Agent Hooks Protocol: `/ahp` accepts native events from arbitrary harnesses through the typed AHP Go SDK.
- Legacy provider endpoints: `/rpc/hooks.claude`, `/rpc/hooks.cursor`, `/rpc/hooks.codex`, and Claude OTEL ingestion. These keep existing installed hooks working.
- Unified ingest: `/rpc/hooks.ingest`. Latest generated hooks use this endpoint and translate provider-native events into Speakeasy feature events before sending.

## Unified Ingest

`/rpc/hooks.ingest` is the stable backend contract for hooks. It is authenticated only with `Gram-Key` and `Gram-Project` using the `hooks` key scope. The backend attributes all events to the authenticated token owner from `AuthContext`; source-reported user fields are not used for identity.

The payload is feature-first:

- `schema_version`: currently `hook.ingest.v1`
- `source`: adapter metadata such as `adapter`, `adapter_version`, `raw_event_name`, and `hostname`
- `session`: provider-independent session and turn identity
- `event`: canonical feature event, for example `prompt.submitted`, `tool.requested`, `skill.activated`, or `notification.reported`
- `data`: feature payload blocks such as `prompt`, `tool_call`, `mcp`, `usage`, `message`, `skill`, and `notification`
- `raw`: original provider payload for debugging only

Provider-specific logic belongs in generated hook glue code and shared bash helpers. The backend dispatches by canonical Speakeasy feature events and data blocks, not by Claude/Cursor/Codex payload shape.

The response is provider-neutral:

```json
{ "decision": "allow" }
```

or:

```json
{ "decision": "deny", "reason": "policy_denied", "message": "..." }
```

Generated hooks translate that response into the local provider response shape.

## Agent Hooks Protocol

`POST /ahp` accepts typed AHP SDK 0.7.0 frames using the existing hooks key and
project authorization. Harness names remain the reported source adapter names.

- `user.message.inbound` owns prompt capture and prompt policy. Only user-role
  envelopes in `message.messages` contribute prompt text. Interception evaluates
  policy only when the harness advertises `deny`; observation never evaluates it.
- `model.response.after` owns assistant transcript capture. Each assistant
  envelope produces a separate, ordered `assistant.responded` event with a
  replay-stable identity. Only inline text parts with category `text` or no
  category are included; reasoning, tool-call, and unknown categories are not.
- Model response usage stays on the original `usage.reported` event, never on
  derived transcript events. Only per-attempt amounts are counted; costs must be
  reported in USD. Replaying the response does not add usage or transcript rows.
- `turn.start`, `turn.end`, and `user.message.outbound` do not duplicate prompts
  or assistant messages. Intercepted `turn.start` is acknowledged without policy.
- Tool request enforcement and executed tool result capture retain their existing
  behavior. Tool output is not subject to the assistant text category filter.

Content references are not fetched, and this adapter does not implement uploads.

## Legacy Compatibility

The legacy Claude path still uses the Redis-buffered validation pattern for installations that depend on Claude OTEL metadata:

1. Unauthenticated Claude hook events may arrive before identity is known.
2. Authenticated OTEL logs populate `session:metadata:{session_id}`.
3. Buffered hook events are replayed once metadata is available.

Do not remove or change these compatibility paths until installed legacy hooks have a migration path.
