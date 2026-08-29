# Hooks Service

The hooks service supports two hook generations:

- Legacy provider endpoints: `/rpc/hooks.claude`, `/rpc/hooks.cursor`, `/rpc/hooks.codex`, and Claude OTEL ingestion. These keep existing installed hooks working.
- Unified ingest: `/rpc/hooks.ingest`. Latest generated hooks use this endpoint and translate provider-native events into Gram feature events before sending.

## Unified Ingest

`/rpc/hooks.ingest` is the stable backend contract for hooks. It uses `Gram-Key` and `Gram-Project` with the `hooks` key scope for transport authentication. Source-reported user fields are not authoritative for the covered `ai_access` acting-user identity, though they can influence attribution and other policy decisions. Approved live `ai_access` checkpoints additionally require a short-lived, proof-bound acting-user assertion and revalidate active organization membership.

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

## AI Access Enforcement Coverage

`ai_access` enforcement is limited to live Claude Code and Codex `UserPromptSubmit` and `PreToolUse` events sent through unified ingest by a relay implementing `hooks-acting-user.v1`. It excludes `PermissionRequest`, replayed or backfilled activity, legacy endpoints, and all other providers and events. No released relay version is claimed until the proof-bound relay ships.

The external real-client E2E suite (not checked into this repository) records its exact binaries in its generated `artifacts/ai-access-versions.json`. On macOS 26.5.2 arm64, the validated development relay reported `speakeasy-hooks dev`; the native clients were Claude Code 2.1.250 and Codex CLI 0.150.1. These are tested versions and platform details, not minimum supported-version claims. Without valid browser-session enrollment and per-invocation proof, a covered live action fails closed with the identity-verification denial.

## Legacy Compatibility

The legacy Claude path still uses the Redis-buffered validation pattern for installations that depend on Claude OTEL metadata:

1. Unauthenticated Claude hook events may arrive before identity is known.
2. Authenticated OTEL logs populate `session:metadata:{session_id}`.
3. Buffered hook events are replayed once metadata is available.

Do not remove or change these compatibility paths until installed legacy hooks have a migration path.
