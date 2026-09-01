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

`ai_access` enforcement is limited to live Claude Code and Codex `UserPromptSubmit` and `PreToolUse` events sent through unified ingest by a relay implementing `hooks-acting-user.v1`. It excludes `PermissionRequest`, replayed or backfilled activity, legacy endpoints, and all other providers and events. No released relay version is claimed without retained release evidence.

Proof-bound enrollment mints an internal API-key scope that public key creation cannot request. Unified ingest uses that server-authenticated marker, rather than only caller-controlled adapter and raw-event fields, to identify an approved relay. Canonical prompt/tool work from that relay stays governed when a provider or raw-event discriminator is missing or changed; the mismatch then fails closed against the server-signed assertion binding. Proofless legacy hook keys remain outside this coverage.

Hook `ai_access` rollout is independent of `mcp-killswitch-shadow` and `mcp-killswitch-enforce`. Those existing flags control only the MCP checkpoint. The hook checkpoint is always constructed and evaluates every covered proof-bound live action; no hook rollout flag is introduced.

The opt-in real-client suite (`mise run hooks:e2e --providers claude,codex --suites ai-access`) records relay and native-client version output in the run's generated `artifacts/ai-access-versions.json`. Those runtime artifacts are not retained in this repository, so the checked-in contract makes no exact native-client, platform, or minimum-version compatibility claim.

The real-client suite drives installed Claude and Codex hooks through the production relay, assertion mint, unified-ingest, server decision, native-denial, and persistence paths. Its non-interactive setup calls the same session-authenticated PKCE authorize/redeem endpoints as relay login, but it cannot automate the production dashboard's browser organization-selection page or the browser-to-localhost callback. Callback transport and credential persistence are covered separately by relay login tests; server proof, tenant, membership, evaluator-failure, discriminator-tampering, and duplicate-delivery acceptance are covered by integration tests. This is a split acceptance boundary, not a claim that one test crosses the browser callback end to end.

## Legacy Compatibility

The legacy Claude path still uses the Redis-buffered validation pattern for installations that depend on Claude OTEL metadata:

1. Unauthenticated Claude hook events may arrive before identity is known.
2. Authenticated OTEL logs populate `session:metadata:{session_id}`.
3. Buffered hook events are replayed once metadata is available.

Do not remove or change these compatibility paths until installed legacy hooks have a migration path.
