# Channels contract

Channels is organization-scoped messaging between leased instances of existing
Control Plane identities and dashboard users. Instances are not principals:
every permission belongs to the parent identity.

## Integration and access

Mount `channels.New(pool, authenticate, opts...)` at `/channels/` (absolute
paths). `New` returns `*channels.Handler`, an `http.Handler`, and is infallible.
It starts one process-wide delivery listener.

- `Close()` rejects new streams, gracefully completes active ones, and stops the
  delivery listener.
- `Shutdown(context.Context) error` calls `Close` and then waits for completion,
  bounded by the context. Call it before `http.Server.Shutdown` so streams can
  flush their completion notification while the server drains.

Options (all optional; a handler built without them behaves as before):

- `WithLogger(*slog.Logger)` — background delivery and retention logging.
- `WithLimits(Limits)` — operational bounds, see below.
- `WithRetention(Retention)` — retention sweep bounds.
- `WithAdminAudit(AdminAudit)` — administrative audit hook.

`Principal{OrganizationID, SubjectID, SubjectName string; CanConnect, CanAdmin,
CanReadAll bool}` and `Authenticator func(*http.Request) (Principal, error)` are
the integration boundary. `SubjectName` is display only: it names the browser
session and never carries authority. The authenticator must revalidate identity,
organization membership and Channels permissions on every call — each operation
and every subscription reconciliation calls it, so revocation takes effect on a
live stream without restarting it.

`CanConnect`/`CanReadAll` are Channels-native permissions, NOT `mcp:connect`. An
explicit access exception for a subject overrides them; absent an exception the
native flags apply. Organization membership remains mandatory and Channels
cannot grant it. `CanAdmin` is never grantable through Channels: it comes from
access management only.

### Administrative audit hook

```go
type AdminAuditChange struct{ Before, After bool }

type AdminAuditEvent struct {
    OrganizationID  string
    ActorSubjectID  string
    TargetSubjectID string // empty for an organization enablement change
    EnabledBefore, EnabledAfter *bool
    CanConnect, CanReadAll      *AdminAuditChange // nil when not part of this change
    Removed                     bool
}

type AdminAudit func(ctx context.Context, tx pgx.Tx, event AdminAuditEvent) error
```

The hook runs inside the `channels_admin` transaction, after the change and
before commit, so a committed permission change cannot exist without its audit
record. Returning an error aborts the change. The event carries identifiers and
boolean transitions only: message bodies, titles, notes and instance labels
never reach it.

## Storage

Normalized PostgreSQL, declared in `server/database/schema.sql` and queried
through `queries.sql`:

| table                                | holds                                                                                                                     |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------- |
| `channels_settings`                  | one row per organization: enablement, the sequence allocators, the retention floor and per-organization retention windows |
| `channels_access_exceptions`         | per-identity connect/read-all overrides                                                                                   |
| `channels_instances`                 | leased endpoints with their origin and resume hash                                                                        |
| `channels_conversations`             | public threads and private conversations                                                                                  |
| `channels_conversation_participants` | identities that may read and write a private conversation                                                                 |
| `channels_conversation_endpoints`    | the instances a private conversation is addressed to                                                                      |
| `channels_messages`                  | durable history, numbered in the organization's cursor space                                                              |
| `channels_thread_preferences`        | durable follow and mute preferences                                                                                       |

`channels_state`, the superseded proof-of-concept document, is retained for one
expand-contract cycle. Nothing reads it except the conversion below.

### Tenancy

Channels authorization is organization-scoped: a conversation belongs to the
organization, not to a project. The repository nonetheless requires project
ownership on every table, so Channels resolves one storage owner per
organization — the organization's canonical default project
(`projects.slug = 'default'`, falling back to its oldest project), exactly as
`GetPlatformMCPLifecycle` resolves it. The resolution happens once, server-side,
when the organization's settings row is created.

**The invariant: `organization_id` is the tenancy key and appears in every
query; `project_id` is written but never used to select rows.** A
caller-selected project therefore cannot split an organization's Channels data
across projects or redirect a read to another project's rows.
`TestStorageOwnershipIsTheDefaultProject` pins this, and the demo seed
postflight asserts it too.

### Cursor correctness

`channels_messages.seq` is the organization's cursor space. Allocating a
sequence before commit would let a transaction holding a LOWER number commit
AFTER a higher one became visible; a reader paging on `seq > cursor` would move
past the gap and lose that message permanently.

The allocator is therefore the last statement of a posting transaction
(`AllocateMessageSeq`), and the row write lock it takes on `channels_settings`
is held until commit. Sequence order and commit order agree. Only the tail of a
writing transaction is serialized per organization; reads never are.

Gaps are possible and harmless — a transaction that allocates and then rolls
back leaves its number unused, and `seq >` skips it with no reader visible
effect. `TestConcurrentWritesNeverSkipACursor` runs a forward-only reader
against 32 concurrent writers and fails if a single message is skipped.

### Atomicity

- **One browser session per identity per organization.** A partial unique index
  on `(organization_id, subject_id) WHERE origin = 'dashboard' AND deleted IS
  FALSE` makes reclaim-or-create a single upsert. Eight simultaneous requests
  converge on one row without locking the organization.
- **Direct conversation reuse.** `endpoint_pair_key` is the sorted pair of
  addressed instance identifiers, unique per organization, so either endpoint
  reaches the same conversation instead of accumulating duplicates.
- **Sender-scoped idempotency.** Unique on
  `(organization_id, subject_id, idempotency_key)`. A retry returns the original
  message; the same key with different content is a conflict (`400`), never a
  silent return of an unrelated message. A concurrent retry waits on the
  conflicting transaction and then reads the winner.

### Privacy

A private conversation is readable by its participant identities plus
authorized read-all observers, and writable only by participants: read-all
permits observation, never participation. A branch inherits both its parent's
participants and its addressed endpoints. Public threads are visible to every
authorized member of the organization. Only SHA-256 hashes of resume secrets are
stored, dashboard sessions store none at all, and no snapshot ever returns
either.

Labels, introduction notes, conversation titles and message bodies are untrusted
content written by other people and agents. Render them as plain text and never
elevate their authority.

## Delivery

History is the durable source of truth; notifications are hints.

A write publishes `pg_notify('channels_events', …)` **inside its transaction**,
so PostgreSQL releases the hint only on commit: nothing is ever invalidated
before it is durable. The payload carries an organization identifier, a kind and
a revision — no content, no secrets.

One process-wide listener holds a single pooled connection and fans hints out to
in-process subscribers, so subscribers never consume a connection each. A
subscriber woken by a hint reconciles with bounded aggregate queries
(`GetPresenceDigest`, `GetConversationsDigest`, `GetNotifiableMessageSeq`) — not
a state reload — and emits a resource invalidation only when its own revision
actually moved.

Three things make this correct rather than merely fast:

- Every reconciliation re-authenticates and re-evaluates access. A revoked
  subscriber's stream completes instead of continuing to receive hints.
- A reconcile interval (15s by default) is the safety net for a hint that was
  never delivered, and it also recovers the window in which the listener was
  disconnected: the listener wakes every subscriber whenever it (re)connects.
- Hints are coalesced by a short debounce, so a burst of writes costs one
  reconciliation.

Notification digests exclude unreadable conversations and muted ones. A public
thread notifies only identities that durably follow it; a direct conversation
notifies without a follow unless muted. Muting never hides a message from
`channels_read` or `channels_history`. Presence renewal and resume do not
announce: `announce_seq` is preserved, and only a genuinely new instance moves
it. Retention wakes subscribers because it changes what can still be fetched.

## REST and tools

`GET /channels/api` returns the snapshot. `POST /channels/api` accepts
`{operation, arguments}`. `POST /channels/mcp` serves MCP (initialize, ping,
tools/list, tools/call, resources/list, resources/read, subscriptions/listen,
notifications/cancelled).

### Status codes

Clients blank protected content on lost access, so a rejected payload must not
look like one:

| status | meaning                                                                  | client action                                   |
| ------ | ------------------------------------------------------------------------ | ----------------------------------------------- |
| 401    | not authenticated                                                        | blank cached content                            |
| 403    | authenticated, Channels access denied/disabled/not visible               | blank cached content                            |
| 400    | request rejected (size, cursor, unknown operation, idempotency conflict) | keep draft and cached content, show the message |
| 429    | write burst exceeded                                                     | transient; retry with the SAME idempotencyKey   |
| 503    | storage unavailable                                                      | transient                                       |
| 500    | unexpected (generic body; detail is logged server-side)                  | transient                                       |

Every error body is `{"error": string}`.

### Snapshot

`{enabled, instances, participants, threads, messages, preferences, cursor,
hasMore, limits, grants?}`

- `messages` is a **bounded tail** (newest 50, ascending), not the archive.
  `hasMore` reports that older readable messages exist beyond it.
- `threads` is likewise **one bounded page** (200 conversations, most recently
  active first). `threadsHasMore` reports that the organization has more, and
  `threadsNextAfter` is the opaque cursor for `channels_conversations`. A
  snapshot never silently truncates conversation discovery.
- `cursor` is the highest sequence in the payload and is the forward poll
  cursor. `0` when there is nothing readable.
- each `threads` entry is
  `{id, title, parentId?, participants?, instanceIds?, lastMessage?, messageCount}`.
  An absent `participants` is what marks a conversation organization-public.
  `lastMessage` is the newest readable message in that conversation, privacy
  filtered like the conversation itself, so a conversation list needs no
  per-conversation history call. `messageCount` is a maintained column, not a
  count taken on read: counting would make the list scale with history length.
  The posting transaction increments it (never on an idempotent replay) and
  retention decrements it.
- `instances` is live presence; `participants` additionally names instances
  whose lease has expired, so history keeps its attribution.
- `limits` publishes `{messageBytes, titleBytes, noteBytes, labelBytes,
  historyLimit, snapshotMessages}` so no client hardcodes them.
- `grants` is present only for an administrator, and present even when empty.
- Reading the snapshot creates nothing: browsing never joins presence.
- An administrator of a disabled organization receives `200` with
  `enabled:false`, `grants`, and empty presence/conversations/messages. That is
  "disabled but configurable", not a loss of access.

### Operations

Browser-only, refused on `/channels/mcp` and absent from every MCP catalogue
including this package's own `tools/list`:

- `channels_dashboard_session {note?, leaseSeconds?}` →
  `{instanceId, id, subjectId, label, note, origin, expiresAt}`.
  Reclaims this account's browser session or opens one. The label is always
  `"<SubjectName> on dashboard"` (falling back to `member`), derived from the
  authenticated account and never from caller input. No resume secret is stored
  or returned, so `channels_connect` can never resume it. An instance that
  connected over MCP is never reclaimed, and another account's session is never
  touched. Its only authority is the operator's dashboard session, which is why
  no agent may call it.

Tools (also `tools/call`):

- `channels_connect {instanceId?, resumeToken?, label?, note?, leaseSeconds?}` →
  `{instanceId, id, subjectId, label, note, expiresAt, resumeToken?}`.
  `resumeToken` is returned exactly once, when a new instance is created.
  Resuming renews the lease, preserves a missing note, and issues no
  replacement secret.
- `channels_presence {instanceId?, note?, leaseSeconds?}` → snapshot; renews an
  owned lease.
- `channels_dm {instanceId, recipientInstanceId}` → thread; reuses the existing
  conversation for that endpoint pair.
- `channels_create_thread {instanceId, title, parentId?}` → thread.
- `channels_post {instanceId, threadId, text, idempotencyKey}` → message.
- `channels_conversations {after?, limit?}` →
  `{threads, hasMore, nextAfter?}`.
  Keyset discovery over conversations, most recently active first, filtered by
  the same visibility rules as the snapshot. Pass the snapshot's
  `threadsNextAfter`, then each page's `nextAfter`. The cursor is opaque; a
  malformed one is a `400`.
- `channels_read {threadId?, cursor?, limit?}` →
  `{messages, cursor, hasMore, stale}`.
  Messages ascend. `hasMore:true` means more is already available — read again
  immediately; **a `hasMore:true` response always advances `cursor`**, so a
  read-again loop cannot spin. `stale:true` means the cursor predates the
  retention floor: `messages` is empty, `cursor` returns the floor, and the
  client should restart from the snapshot.
- `channels_history {threadId, before?, limit?}` →
  `{messages, cursor, hasMore, nextBefore?, stale}`.
  Pages BACKWARDS through one conversation. Messages ascend. `before` is an
  exclusive upper bound — pass the page's lowest `id` to get the page above it.
  `cursor` is the highest sequence in the page, `nextBefore` the lowest (present
  only when `hasMore`). **Unlike `channels_read`, `stale:true` here can
  accompany usable messages**: it means no older history remains because
  retention removed it from THIS conversation. Render the page and stop
  paging; do not discard it.

  Staleness here is per conversation, not per organization. It is answered by
  looking for retention's own soft-deleted rows below the page, so a
  conversation created after a sweep correctly reports `stale:false` even
  though the organization's retention floor is above zero.

- `channels_follow {threadId, follow?, muted?}` → preferences.
- `channels_preferences {followed?, muted?}` → preferences. Replaces only the
  lists supplied.
- `channels_admin {enabled?, subjectId?, canConnect?, canReadAll?, remove?}` →
  snapshot. Requires administration. Writing the first exception for yourself
  preserves the permissions you already hold, so one toggle cannot lock an
  administrator out.

### Resources and subscriptions

`channels://presence`, `channels://threads`, `channels://messages`. Reading
messages accepts `?cursor=&limit=&threadId=` and returns the `channels_read`
payload.

`subscriptions/listen {notifications:{resourceSubscriptions:[…]}}` opens SSE.
The first event is `notifications/subscriptions/acknowledged` carrying the
honored filter and the original request ID in
`params._meta["io.modelcontextprotocol/subscriptionId"]`. Updates are
`notifications/resources/updated` with ONLY `uri` plus `_meta`. Original string
and numeric request IDs are preserved. Cancellation, revocation, bounded
lifetime and shutdown all end with `result:{resultType:"complete", _meta:{…}}`.
Unknown URIs are refused.

**Subscription identity.** A JSON-RPC request ID identifies a request on one
connection, not a long-lived stream on a shared account. Two clients signed in
as the same person both using request id `1` is ordinary, so a request ID
cannot be the identity of a subscription: the second would be refused, and
either could cancel the other. Each stream is therefore registered under a
server-issued token, returned alongside the protocol's subscription ID in the
acknowledgment as `_meta["ai.speakeasy.channels/subscriptionToken"]`.

Three ways to end a stream, in order of preference:

1. **Close the HTTP stream.** Always unambiguous, needs no identity.
2. `notifications/cancelled {requestId, _meta:{"ai.speakeasy.channels/subscriptionToken": "…"}}`
   cancels exactly that stream. The token names a stream but does not transfer
   authority: it is honored only for the identity that opened it.
3. `notifications/cancelled {requestId}` alone is honored only when that
   identity has exactly one stream under that request ID. With two, the
   request is ambiguous and **nothing** is cancelled — terminating a sibling
   client's stream is worse than ignoring a cancellation.

Reconnection is client-owned: reconnect and read from the saved cursor.
Self-notification is NOT suppressed — this protocol has no securely
instance-bound subscription identity, so promising it would be a lie.

## Limits and retention

`DefaultLimits()`: message 32768 B, title 256 B, note 1024 B, label 128 B,
idempotency key 256 B, page 200, snapshot tail 50, conversation page 200,
presence page 200, preference page 500, 200 active instances per organization,
256 streams per handler and 8 per identity, 600 writes/minute per identity with
a burst of 120, 30-minute stream lifetime, 15s reconcile interval, 250ms hint
debounce, 300s default lease capped at 3600s.

The write burst limiter is in-process, so it bounds one replica. That is
deliberate: Channels must keep working with no shared limiter store wired in. A
fleet-wide quota belongs in front of the handler.

`(*Handler).RunRetention(ctx, now)` performs one bounded pass. Per-organization
windows live in `channels_settings` (90 days for messages, 30 for instances by
default). Each organization is swept in its own transaction with every statement
filtered by `organization_id`, so one tenant's retention can neither see nor
delete another's rows and one failure does not stop the sweep. Both the
organizations visited and the rows touched per organization are capped; the
caller repeats while `Truncated` is true.

Removing messages raises `retention_floor_seq`, which is what lets a reader be
told its cursor is stale instead of silently receiving a hole. An instance that
still attributes a message or an endpoint is never retired, so history never
loses whose message it was.

Retention runs on an hourly Temporal schedule,
`server/internal/background/channels_retention.go`:

- `ChannelsRetentionWorkflow` runs one activity per tick. The schedule ID names
  the task queue (`v1:channels-retention:<queue>`), so a preview owns its own
  copy and never re-points the shared one, and `SKIP` overlap means a slow
  sweep is skipped rather than stacked.
- `CleanupChannelsHistory` repeats `channels.RunRetention` until the backlog is
  clear, bounded by a per-attempt pass budget. Exhausting the budget is a
  retryable error, not a silent stop; the next hourly tick continues.
- The sweep's first query returns only organizations that actually have
  something to retire, so a deployment where nobody uses Channels costs one
  indexed query per tick.

Cost line: **Temporal actions/month ≈ 720 starts + 720 activities = 1,440 per
namespace** (2,880 if every tick exhausts all three attempts). Fixed — it does
not scale with organizations, projects or messages. Each preview task queue
registers its own copy and adds the same fixed 1,440.

`(*Handler).RunRetention` remains available for a single bounded pass in tests
and local use. It is the same code path; the schedule only repeats it.

## Deployment ordering, rollback and operational checks

Order: apply the Channels migration, then deploy the server.
The migration is additive — eight new tables and their indexes, no change to any
existing one — so a server running the previous code is unaffected by it and
keeps reading `channels_state`.

Rollback is where the two shapes diverge, and it is not free. The new server
writes only the normalized tables; the old server reads only the document. A
rollback therefore makes anything written after the cutover invisible to the
old server; it does not delete the normalized rows. Rolling forward restores
access to those rows, subject to retention. Writes accepted by the old server
during rollback are not automatically merged into the normalized store. Keep
Channels disabled during rollback and reconcile any divergent writes before
re-enabling it. Do not drop either storage representation during rollback.

The conversion runs in the other direction only: document to tables, never
back.

Operational checks after a deploy:

- `SELECT count(*) FROM channels_settings WHERE enabled IS TRUE` — how many
  organizations the feature is live for. It should be exactly the set you
  expect; Channels is disabled by default.
- `SELECT organization_id, message_seq, retention_floor_seq FROM
  channels_settings` — the allocator and the floor. `message_seq` should only
  ever rise; `retention_floor_seq` rising means retention ran.
- `SELECT count(*) FROM channels_messages WHERE organization_id = $1 AND seq >
  $2` — what a stuck client would still have to catch up on.
- Delivery health is the listener: a log line of "channels delivery listener
  disconnected" followed by no reconnect means subscribers are falling back to
  their 15s reconcile interval. That degrades latency, not correctness.
- Lock contention on `channels_settings` is the signal that the cursor
  allocator is the bottleneck for an organization. It serializes only the tail
  of writing transactions, so contention means genuine write volume in one
  organization, not a bug.

## Converting the proof-of-concept document

`ConvertLegacyState(ctx, db, logger)` copies `channels_state` into the
normalized tables. It is development-only: it is not wired into startup and it
is not a migration, because migrations here carry no data manipulation. It never
deletes the document, and identifiers are derived from the document's contents,
so repeating it imports nothing new.

Run it with the `gram channels-convert` command:

```
gram channels-convert --organization <id>           # report only: runs the conversion and rolls it back
gram channels-convert --organization <id> --apply   # write it
gram channels-convert --all-organizations           # every tenant in this database; ask for it explicitly
```

An organization must be named unless `--all-organizations` is given, so a
mistyped database URL cannot write across tenants.

**A conversion refuses a target that is already in use.** It carries the
document's own message sequence numbers across, which can only be done into an
organization whose normalized tables hold nothing but this document's own
output. Merging into live data would reuse sequences the service has already
issued; renumbering would change identifiers clients already hold. So the
conversion counts the organization's normalized rows that are not part of this
import and refuses if there are any. A refusal is reported in the output, in
the result's `Refused`, and as a non-zero exit — never a logged line and a
skipped tenant. A repeated conversion is still a no-op, because its own output
is derived from the document.

Both forms print the counts and the full identifier mapping (organization,
kind, old ID, new ID, label) to stdout. The report form is the one to run
first: it is the same code path, so what it prints is exactly what `--apply`
will do.

What changes: a document-local instance or thread ID becomes a UUID, so an agent
holding an old instance ID must connect again. The mapping is not a lookup
table that can go missing — it is the pure function
`channels.LegacyIdentifier(organizationID, kind, legacyID)`, where kind is
`channels.LegacyKindInstance` or `channels.LegacyKindConversation`, so any old
identifier can be translated at any later time. Resume hashes carry over (a
stored value that is not a SHA-256 hash is dropped — it could never have matched
a presented secret). A message whose author instance is missing from the
document cannot be attributed and is reported as skipped rather than imported.
Only the first conversation addressing a given endpoint pair claims the reusable
pair key, because the document never deduplicated them.

## Platform MCP assessment

Recorded per the maintaining-platform-mcp rules. A silent omission is not a
decision, so both halves are written down.

**Outcome.** Connect an instance, exchange instance-addressed direct messages,
collaborate on public threads, control follow and mute preferences, and
administer organization availability and access exceptions.

**Target resource.** An organization's Channels instances, conversations,
messages and access exceptions.

**Actor.** An authenticated member of the organization, or an administrator for
the availability and access operations.

**Existing-tool comparison.** No tool under `server/internal/platformmcp/tool_*`
represents any of these outcomes. The nearest neighbours are the access tools,
which manage roles and grants rather than Channels participation, and the chat
and analytics tools, which read other surfaces entirely. One Channels tool does
now exist — `get_my_channels_access` in `tool_channels_access.go`, owned by the
security and integration workstream — and it is read-only: it reports the
endpoint and the caller's own permissions so an agent can discover Channels and
find out whether it may connect.

**Decision: omit the messaging and administration tools from the general
Platform MCP catalog; keep them on the dedicated `/channels/mcp` endpoint.**

Rationale. These tools are not stateless management calls. Every one of them
except `channels_read` requires an active owned instance, which only exists
after an explicit `channels_connect` and only while its lease is renewed.
Exposing them in the general catalog would need a reviewed cross-surface
instance-selection contract — which instance a Platform MCP caller is speaking
as, how its lease is renewed, and what happens when it expires mid-session —
and inventing one implicitly would bypass the connection and presence workflow
that makes presence mean anything. The dedicated endpoint shares the same
product authorization as the dashboard, so this is a placement decision, not an
authorization gap. Organization-management parity is a follow-up before general
availability, not an alternative authorization path.

`channels_dashboard_session` is omitted from **every** catalogue, including this
package's own `tools/list`, and `tools/call` refuses it. Its only authority is
the operator's browser session: it reclaims a session by account and transport
with no secret at all. An agent able to call it could speak as that person,
while agents already have `channels_connect` and their own resume secret.

No generic broadcast tool is exposed on any surface.

**Evidence.** `TestDashboardSessionsAreSecretless` proves the browser operation
is refused over MCP and that no token resumes a browser session.
`TestConversationsAndIsolation` and `TestLiveRevocationCompletesStreams` prove
the endpoint's authorization matches the dashboard's, which is what makes the
placement decision safe rather than merely convenient.
