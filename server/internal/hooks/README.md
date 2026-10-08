# Hooks Service

The hooks service supports three hook entry points:

- Agent Hooks Protocol: `/ahp` accepts native AHP events through the Go SDK, including events from previously unknown harnesses.
- Legacy provider endpoints: `/rpc/hooks.claude`, `/rpc/hooks.cursor`, `/rpc/hooks.codex`, and Claude OTEL ingestion. These keep existing installed hooks working.
- Unified ingest: `/rpc/hooks.ingest`. Latest generated hooks use this endpoint and translate provider-native events into Gram feature events before sending.

## Unified Ingest

`/rpc/hooks.ingest` is the stable backend contract for normalized hooks. Senders use `Gram-Key` and `Gram-Project` with the `hooks` key scope. Keyless requests are acknowledged without processing; this retained compatibility acknowledgement is not an evaluated policy approval. Invalid presented credentials are rejected. AHP does not use this optional-auth path: `/ahp` and `/ahp/content` reject missing credentials. Actor attribution follows the shared hook processor's existing source-reported developer-email rules; that attribution is not proof of authenticated user identity. The hooks key authenticates tenant/project access, not the reported developer. AHP does not populate these source-reported developer fields: its `source` remains correlation metadata, and authenticated principal scoping is separate.

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

## Agent Hooks Protocol

AHP is an additional transport into the shared hook processor, not a replacement for `/rpc/hooks.ingest`. The implementation pins the Go SDK and its matching draft protocol. The endpoint accepts one JSON-RPC object per HTTP POST with `Content-Type: application/json`:

- `hooks/intercept`: synchronous processing with a correlated response containing supported effects. Empty effects request no change; they do not grant permission.
- `hooks/observe`: observation without returned effects or a blocking decision. An observed proposal is not proof that an operation executed.
- `hooks/capabilities`: endpoint capability discovery without creating a session or executing work.

Harness identity and AHP transport provenance remain separate. External OTLP ingestion strips the reserved `gram.hook.schema`, `gram.hook.canonical_event`, `gram.hook.transport`, and `gram.hook.usage_authority` markers before publishing or forwarding records; only trusted in-process hook processing can stamp canonical provenance. Historical source, event, and hostname metadata remain supported. A new harness does not need a server-side registration or an entry in a known-harness list to become observable. Events with equivalent canonical semantics use existing processing; other valid AHP events remain generic observations.

### Authentication and project selection

Use the existing hooks API key with the `hooks` scope. AHP accepts it as `Authorization: Bearer <HOOKS_KEY>`; integrations that can set custom headers may continue using `Gram-Key`. Project selection uses the existing `Gram-Project` authorization path, including its bound-project checks. As on normalized ingest, an omitted project is resolved only when the organization has exactly one project; omission is not permission to choose an arbitrary project.

The AHP event source, session identifiers, and content references do not establish identity or project authorization. Upload credentials are resolved independently from event credentials; configure the same hooks key explicitly for both endpoints rather than assuming inheritance.

### Native registration

The following registration uses standard AHP fields and an organization with one project. For a multi-project organization, the integration must supply an explicit project selector. Set `SPEAKEASY_HOOKS_KEY` outside the registration file; do not put the token in a URL or checked-in configuration.

```json
{
  "protocolVersion": "draft",
  "hooks": [
    {
      "id": "com.speakeasy.gram",
      "transport": {
        "type": "http",
        "url": "https://ai.speakeasy.com/ahp"
      },
      "authentication": {
        "type": "bearer",
        "tokenEnv": "SPEAKEASY_HOOKS_KEY"
      },
      "subscriptions": [
        {
          "events": ["tool.before", "turn.start"],
          "mode": "intercept",
          "timeoutMs": 5000,
          "failurePolicy": "fail-closed",
          "content": { "default": "metadata", "text": "body" },
          "upload": {
            "endpoint": "https://ai.speakeasy.com/ahp/content",
            "timeoutMs": 5000,
            "maxBytes": 65536,
            "auth": { "type": "bearer", "tokenEnv": "SPEAKEASY_HOOKS_KEY" }
          }
        },
        {
          "events": ["*"],
          "mode": "observe",
          "content": { "default": "metadata", "text": "body" },
          "upload": {
            "endpoint": "https://ai.speakeasy.com/ahp/content",
            "timeoutMs": 5000,
            "maxBytes": 65536,
            "auth": { "type": "bearer", "tokenEnv": "SPEAKEASY_HOOKS_KEY" }
          }
        }
      ]
    }
  ]
}
```

Select intercept events only when the harness advertises that boundary and an enforceable denial. The example's failure policy is for a fail-closed organization; distribution must substitute the actual organization setting. Body selection does not grant content access: the harness must independently authorize disclosure. Keep native payloads and non-text bodies disabled unless their processing is explicitly supported.

`/ahp/content` verifies upload framing, exact byte size, SHA-256, and completed reads before allocating an opaque reference. References are scoped to the authenticated organization, project, and principal. The receiver never fetches a URL supplied as a reference. Textual content is bounded to 64 KiB per upload and 256 KiB per event. Resolution allows at most 128 distinct references per event and a one-second aggregate cache-read budget; exceeding either limit makes the selected content unavailable rather than partially materializing it. At most 32 uploads can be read concurrently per service instance; excess requests receive HTTP 429 before body reads. References expire after 10 minutes; retained-content admission is limited to 4,096 uploads per authenticated scope within that window (at most 256 MiB of uploaded bodies). Expired or unavailable references produce a coverage gap rather than fabricated content.

### Failure policy and coverage

Distribution must set each intercept subscription's `failurePolicy` to match the organization's hooks fail-open/fail-closed setting. The endpoint applies the organization setting when it can respond, but cannot change how a harness handles an unreachable backend or an expired local deadline. Observe subscriptions have no `failurePolicy` or interception timeout.

An explicit policy denial is not an operational failure and remains a denial even for fail-open subscriptions. Unsupported enforcement is retained as observation, not reported as a successful block.

Coverage depends on evidence and the capabilities advertised for each occurrence. Missing content, usage, MCP inventory, or correlation identity means limited coverage, not a successful scan, zero usage, or an empty inventory. Enforcement requires an intercept boundary that can apply the requested effect. Organization hooks fail-open/fail-closed settings govern failures; unsupported enforcement remains observation with a coverage gap. Native host permissions remain authoritative.

Generic analytics eligibility applies to newly ingested canonical events. Historical replay, reclassification, and backfill are outside this change.

The analytics migration replaces two materialized views without changing their destination tables. Coordinate ingestion during rollout: ClickHouse drop/create operations are not atomic and can leave a capture gap or a missing view after a partial failure. Before retrying a partially applied migration, reconcile the actual views with the migration runner's recorded progress; do not edit or rehash an applied migration.

## Platform MCP parity

AHP ingestion is a machine-to-machine event transport, not an administrator action; it does not need a raw Platform MCP ingestion tool. Existing observation and session query tools consume the shared telemetry and conversation data, including arbitrary harness sources.

## Legacy Compatibility

The legacy Claude path still uses the Redis-buffered validation pattern for installations that depend on Claude OTEL metadata:

1. Unauthenticated Claude hook events may arrive before identity is known.
2. Authenticated OTEL logs populate `session:metadata:{session_id}`.
3. Buffered hook events are replayed once metadata is available.

Do not remove or change these compatibility paths until installed legacy hooks have a migration path.
