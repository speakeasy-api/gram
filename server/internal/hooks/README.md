# Hooks Service

The hooks service supports two hook generations:

- Legacy provider endpoints: `/rpc/hooks.claude`, `/rpc/hooks.cursor`, `/rpc/hooks.codex`, and Claude OTEL ingestion. These keep existing installed hooks working.
- Unified ingest: `/rpc/hooks.ingest`. Latest generated hooks use this endpoint and translate provider-native events into Gram feature events before sending.

## Unified Ingest

`/rpc/hooks.ingest` is the stable backend contract for hooks. It is authenticated only with `Gram-Key` and `Gram-Project` using the `hooks` key scope. The backend attributes all events to the authenticated token owner from `AuthContext`; source-reported user fields are not used for identity.

The payload is feature-first:

- `schema_version`: currently `hook.ingest.v1`
- `source`: adapter metadata such as `adapter`, `adapter_version`, `raw_event_name`, and `hostname`
- `session`: provider-independent session and turn identity
- `event`: canonical feature event, for example `prompt.submitted`, `tool.requested`, `skill.activated`, or `notification.reported`
- `data`: feature payload blocks such as `prompt`, `tool_call`, `mcp`, `usage`, `message`, `skill`, and `notification`
- `raw`: original provider payload for debugging only

Provider-specific logic belongs in generated hook glue code and shared bash helpers. The backend dispatches by canonical Gram feature events and data blocks, not by Claude/Cursor/Codex payload shape.

The response is provider-neutral:

```json
{ "decision": "allow" }
```

or:

```json
{ "decision": "deny", "reason": "policy_denied", "message": "..." }
```

Generated hooks translate that response into the local provider response shape.

## Legacy Compatibility

The legacy Claude path still uses the Redis-buffered validation pattern for installations that depend on Claude OTEL metadata:

1. Unauthenticated Claude hook events may arrive before identity is known.
2. Authenticated OTEL logs populate `session:metadata:{session_id}`.
3. Buffered hook events are replayed once metadata is available.

Do not remove or change these compatibility paths until installed legacy hooks have a migration path.
