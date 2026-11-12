# Hooks Service

The hooks service supports four hook entry points:

- Agent Hooks Protocol: `/ahp` accepts native events from arbitrary harnesses for session capture and policy enforcement.
- Legacy provider endpoints: `/rpc/hooks.claude`, `/rpc/hooks.cursor`, `/rpc/hooks.codex`, and Claude OTEL ingestion. These keep existing installed hooks working.
- Unified ingest: `/rpc/hooks.ingest`. Latest generated hooks use this endpoint and translate provider-native events into Speakeasy feature events before sending.
- Transcript upload: `/transcript` stores session transcripts that the hooks binary ships on each Stop when the standard Claude plugin runs inside Claude's Cowork VM. See [Transcript Upload](#transcript-upload).

## Unified Ingest

`/rpc/hooks.ingest` is the stable backend contract for hooks. It is authenticated only with `Gram-Key` and `Gram-Project` using the `hooks` key scope. The backend attributes all events to the authenticated token owner from `AuthContext`; source-reported user fields are not used for identity.

The payload is feature-first:

- `schema_version`: currently `hook.ingest.v1`
- `source`: adapter metadata such as `adapter`, `adapter_version`, `raw_event_name`, and `hostname`
- `session`: provider-independent session and turn identity
- `event`: canonical feature event, for example `prompt.submitted`, `tool.requested`, `skill.activated`, or `notification.reported`
- `data`: feature payload blocks such as `prompt`, `tool_call`, `mcp`, `usage`, `message`, `skill`, and `notification`
- `raw`: original provider payload for debugging only

Every telemetry row the endpoint writes is also republished into the OTel pipeline (the `gram.otel.v1.InboundLogRecord` topic) through `otelpub`, under the hook source as `service.name` and the instrumentation scope `github.com/speakeasy-api/gram/server/internal/hooks`, with the same attributes plus `gram.hook.adapter`, `gram.hook.raw_event_name`, `gram.hook.canonical_event_type`, `gram.hook.permission_type`, `gram.hook.turn_id`, `gram.hook.decision`, `gram.session.id`, `user.id` and `user.email`. The `HookLog` dialect in `server/internal/otel/dialect` recognises those records by their scope and types them (tool hooks as `tool_call` / `tool_call_result`, prompts as `prompt`, permission requests as `tool_decision`, everything else unclassified with its raw name kept), and the column enrichers fill `agent_events` from its answers. The record id is derived from the idempotency key, so a device-spool replay collapses at read time; the publish runs after the response, and a failure is logged but never changes the hook's verdict.

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

`POST /ahp` accepts AHP events with a hooks-scoped API key in
`Authorization: Bearer <HOOKS_SCOPED_API_KEY>` or `Gram-Key`. Use `Gram-Project`
when an explicit project selector is needed. Keep secret-bearing hook
configuration uncommitted.

Call `hooks/configuration` to discover the supported subscriptions:

- Intercept: `turn.start`, `user.message.inbound`, and `tool.before`.
- Observe: `session.start`, `session.end`, `turn.end`,
  `model.response.after`, and `tool.after`.

Each event is advertised once. Intercept subscriptions use a ten-second timeout
and the authenticated organization's current failure policy (`fail-open` or
`fail-closed`); policy presence does not change delivery mode. Both modes request `content: {"default": "body"}`.
The timeout is a fixed transport default; there is no organization-level AHP timeout setting.
Discovery lookup failures return the SDK-native JSON-RPC
error `-32004` (`Backend internal error`) over HTTP 200, without subscriptions or
an assumed failure policy. Authentication failures remain HTTP 401/403.

Observe mode records events without enforcing policy. Prompt and tool
interception enforce denial only when the harness advertises `deny` at that
boundary; otherwise the event is recorded without enforcement. User prompts
arriving through `user.message.inbound` and admitted through `turn.start` both
remain supported. Turn-start ingestion accepts user-role items on user or external
triggers, excluding assistant/reasoning/tool-result context and continuation/hook
turns. Matching message identities correlate transcript rows across the two prompt
boundaries, including arrival before a turn exists, without suppressing independent
policy evaluation. OpenCode single-message prompts also retain their shared
native/proxy identity. Other harnesses need a shared message identity: provider-turn-only
proxy events cannot safely be matched to arbitrary AHP message IDs.
Both subscriptions advertise `/ahp/uploads` with a ten-second timeout and a 1 MiB
limit. Uploads use the same authentication and project as event requests. Binary
uploads are accepted, but only text is processed as prompt attachment evidence.
Unavailable staged references return a backend error, subject to the subscription's
fail-open or fail-closed policy.

To omit local subscriptions, clients must support discovery and approve the upload
destination and credential forwarding. Go SDK hosts use `AuthorizeConfiguration`;
without approval, discovery skips the **entire backend**, including inline-text events.

## Transcript Upload

The standard Claude plugin switches modes by itself. Claude's admin plugin
settings install it in Claude Code and in the Cowork VM alike. When Claude Code
reports the Cowork entrypoint (`CLAUDE_CODE_ENTRYPOINT=remote_cowork`, which
every hook process inherits), prompt, tool and permission events go to
`/rpc/hooks.ingest` as they do in Claude Code, so policy is enforced in real
time and the session is captured per event. The org instruction moves every
unified session into Cowork, so the only message no hook sees is the first,
typed in chat before the move. The binary therefore also ships the transcript
from Stop, which backfills that prompt and the assistant text no hook reports.
Session start, inventory, notification and config events stay local in the VM.

Both paths can describe the same prompt or tool call. Envelopes from the VM
carry `session.surface: "cowork"`, and both paths then key those rows by
the ids Claude reports to both: `claude-prompt:<prompt_id>` (the transcript's
`promptId`), `claude-tool-call:<tool_use_id>` and
`claude-tool-result:<tool_use_id>`. The `(chat_id, external_message_id)` unique
index keeps one row per message whichever path writes first and however often
either is re-sent. Rows only the transcript describes keep the
`claude-transcript:<uuid>` key.

Capture needs both halves deployed: a pinned hooks binary that includes the
Cowork mode, and an ingress rule forwarding `/transcript` to the server.
Production ingress routes only listed prefixes to the server; without the
rule, uploads are answered by the dashboard and stay queued in the VM.

`POST /transcript` uses the same hooks-scoped key and project selection as
`/ahp`. The body's `schema_version` selects the transcript format;
`claude.transcript.v1` (raw Claude Code JSONL lines) is the only one accepted.
This endpoint is the one exception to dispatching on canonical feature events:
the backend parses the provider's own transcript format, because the hooks
never see the parts of a Cowork session the transcript fills in (the first
prompt and the assistant's text).

Prompts and tool calls are keyed by the ids the hooks also use
(`claude-prompt:<prompt id>`, `claude-tool-call:<tool_use id>`,
`claude-tool-result:<tool_use id>`); other entries are keyed by the entry's
uuid (`claude-transcript:<uuid>`), with `/block:<index>` appended when one
entry yields more than one row. A line the hooks already stored, or a re-sent
line, is never stored twice.

### Warn policies in Cowork

A warn on a typed prompt challenges it: the prompt is blocked, and in Claude
Code the block text carries an acknowledgement link the person opens before
retrying. Cowork shows a hook's block text cut off after two lines with no way
to expand it, and shows it as plain text, so a long link is cut off and none
is clickable. For events with `session.surface: "cowork"` the challenge leads
with a short link, `<site>/ack#<token>`, that the person copies into a
browser. The dashboard's `/ack` route redeems it like the long link, and the
person then tries again. The challenge key masks the "Message sent at …"
sentence Claude's apps add to the prompt's reminder, because it names the
minute the message was sent, so a retry a minute later still matches.

The first message is typed in chat before the session moves to Cowork, so no
prompt hook sees it. The Stop that ships it closes that gap. When an upload
stores new rows, the server checks the upload's user prompts from the last 15
minutes (at most five) against the project's enforcing policies and returns the
warn matches as `warnings` beside `stored`. The binary then answers that Stop
with `decision: block` and a reason, so Claude continues once and tells the
user, plus a `systemMessage` carrying the same warning. The next Stop ships no
new prompt and finishes. The prompt has already reached the model, so this
warning is a notice. Block and quarantine matches are enforced by the
per-event hooks, not here. A full re-send stores nothing new and warns nobody
twice.

The binary advances its read offset past a batch only once the batch is
settled, and resends anything unsettled on the next Stop:

- A 2xx carrying the `{"stored": n}` acknowledgement settles the batch as
  stored.
- Any other 4xx (400 malformed body, 409 session owned by another project,
  413 too large) settles the batch as dropped. The server would reject it
  the same way on every retry, so those lines are lost for good. The binary's
  debug log records the status.
- A 5xx leaves the batch queued, but a batch answered 5xx on three Stops in
  a row, with no other answer between them, is dropped, so one entry the
  server cannot store does not hold the offset forever. Only the lines the
  batch held at its first failure are dropped; lines appended since are
  sent again.
- 401, 403, 404, 405, 408, 429, transport failures, and a 2xx without the
  acknowledgement leave the batch queued for retry without limit.
  Credentials, throttling, and routing can recover, and a 2xx from anything
  but this endpoint must not count as stored.

A batch carries at most 500 entries, which store in about a second, so a
long transcript replayed from offset zero advances, and persists its
offset, a batch at a time within one Stop's 15-second budget.

## Legacy Compatibility

The legacy Claude path still uses the Redis-buffered validation pattern for installations that depend on Claude OTEL metadata:

1. Unauthenticated Claude hook events may arrive before identity is known.
2. Authenticated OTEL logs populate `session:metadata:{session_id}`.
3. Buffered hook events are replayed once metadata is available.

Do not remove or change these compatibility paths until installed legacy hooks have a migration path.
