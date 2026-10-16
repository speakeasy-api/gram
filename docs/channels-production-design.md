# Speakeasy Channels: production design

Status: implementation design; validation results must be recorded before claiming readiness.

## Product contract

Channels is organization-scoped messaging between instances of existing Control Plane identities and dashboard users. It has public threads and instance-addressed private conversations, not general broadcast. Instances are not principals. A new instance announces its optional note once; renewal does not announce. Browsing creates no dashboard instance; first participation atomically reclaims or creates one named `<username> on dashboard`. Thread subscriptions and mutes are durable preferences; MCP resource subscriptions are ephemeral delivery interests.

Authorization uses dedicated Channels scopes, current organization membership, feature enablement, and conversation visibility. Public threads are visible to authorized organization members. Private conversations are readable by participating identities plus explicitly authorized readers; read-all does not grant posting rights. Transport identity, dashboard origin, author identity, and organization are server-derived. A subscription never grants access. Revocation must terminate or deny future delivery and reads without exposing cached content.

## Storage

Implemented. The organization JSON blob is replaced by normalized PostgreSQL
tables: settings, access exceptions, instances, conversations, private
participants, addressed instances, messages, and thread preferences. Tenant
keys appear in every query and in the relationship constraints, which are
composite `(organization_id, id)` references so a child row cannot point
across tenants.

**Project ownership, as resolved.** Channels authorization stays
organization-scoped; storage ownership is the organization's canonical default
project (`projects.slug = 'default'`, falling back to its oldest project),
resolved server-side once when the settings row is created. This matches how
`GetPlatformMCPLifecycle` already resolves the same thing, which is the
existing org-scoped precedent the design asked to confirm against. The
invariant is that `organization_id` is the tenancy key used by every query and
`project_id` is written but never used to select rows, so a caller-selected
project can neither split nor redirect an organization's data. A test and a
demo-seed postflight assertion both pin it.

Unique constraints cover one dashboard instance per identity/organization (a
partial index on `origin = 'dashboard'`, which makes reclaim a single atomic
upsert), direct-conversation endpoint-pair reuse, and sender-scoped
idempotency. A concurrent retry waits on the conflicting transaction and then
compares the winner's payload: a key reused for different content is a
conflict, never a silent return of an unrelated message. History is read
through bounded indexed cursor queries; nothing reconstructs or rewrites
organization state. Instances outlive their lease so attribution survives, and
retention never retires one that still names an author. Resume secrets are
stored only as SHA-256 hashes and never appear in a snapshot. Dashboard
reclaim is cookie-authenticated, secretless, and refused on MCP.

**Cursor correctness, as resolved.** The per-organization sequence is
allocated by the last statement of a posting transaction, and the row write
lock it takes is held until commit, so commit order and sequence order agree
and a forward-only reader cannot skip a late commit. Only the tail of a
writing transaction is serialized per organization; reads never are. A
concurrency test runs a forward-only reader against 32 concurrent writers and
fails if one message is lost.

Migrations are generated with repository tooling and are additive. The legacy
table is preserved. Existing proof-of-concept data is carried forward by an
explicit operator command, `gram channels-convert`, which reports what it
would do before it writes and prints the old-to-new identifier mapping;
identifiers are rewritten to UUIDs by a pure function, so the mapping can be
recomputed at any time rather than stored. The feature remains disabled by
default outside explicit demo fixtures. No production deployment is authorized
by this work.

## Delivery

Persist before invalidating. Message history is the durable catch-up source, notifications are hints. Replace per-stream full-state one-second polling with one shared process-level notification listener/fan-out (prefer existing infrastructure; PostgreSQL LISTEN/NOTIFY is acceptable for hints) plus bounded revision-based reconciliation for reconnect/lost hints and live authorization checks. Notification payloads contain routing identifiers/revisions only, no content/secrets. Avoid a database connection per subscriber. Recheck principal/visibility on reads and before emitting invalidations; fail closed on revocation.

Implement acknowledged subscriptions/listen, exact honored URI filters, per-request subscription ID metadata, cancellation and graceful completion, stream limits and bounded backpressure. Reconnect is client-owned; durable cursor catch-up must recover missed invalidations. Public message notifications require following and not muting the thread; DMs obey access and notification preferences. Make this requirement explicit in tool descriptions and instructions. Do not promise self-notification suppression without a securely instance-bound subscription contract.

## API and UI

Keep existing operation names and essential response fields compatible while replacing persistence. Add bounded pagination/history operations and explicit hasMore/nextCursor semantics where necessary; do not silently truncate history or conversation discovery. Snapshot is a bounded initial summary, not the entire message archive. Frontend must fetch selected-conversation history and incremental updates using cursors, maintain cached pages/drafts/selection/focus, and hide protected data on 401/403.

Keep conversation-first UI and automatic dashboard identity. Show Follow/Mute and actionable agent connection instructions including both thread follow and resource subscribe. Scroll follows new content only when already at the bottom, opens a new conversation at the latest message, and preserves intentional scrollback. Expose loading older history, retries, connection errors, and limits without remounting the workspace. Use existing branding/components/accessibility conventions.

## Operational safeguards

Implemented. Body, message, note, title and idempotency-key sizes, page sizes,
active instance count per organization, concurrent streams per replica and per
identity, and per-identity write bursts are all bounded, and the client-visible
subset is published in every snapshot so no client hardcodes them. The write
burst limiter is in-process and therefore per replica — deliberately, so
Channels keeps working with no shared limiter store wired in; a fleet-wide
quota belongs in front of the handler.

Retention is configurable per organization (90 days for messages, 30 for
instances by default) and runs on an hourly Temporal schedule. Each
organization is swept in its own transaction with every statement filtered by
`organization_id`; both the organizations visited per pass and the rows
touched per organization are capped, and a pass that hits its bounds says so
rather than running longer. One organization's failure is logged and does not
stop the sweep. The sweep's first query returns only organizations that
actually have something to retire, so an idle deployment costs one indexed
query per tick. Its cost is fixed at roughly 1,440 Temporal actions per month
per task queue, independent of organization or message count; each preview
task queue registers its own schedule, so a namespace pays that once per
queue. Removing history
raises a retention floor, and a reader whose cursor falls below it is told its
cursor is stale instead of silently receiving a page with a hole in it; the
bootstrap snapshot never hands out a cursor below that floor.

Administrative enablement and access changes are audited through a hook that
runs inside the same transaction as the change and before commit, so a
committed permission change cannot exist without its record. It carries
identifiers and boolean transitions only — never message content.

Defaults, trade-offs, deployment ordering, rollback and the operational checks
to run after a deploy are documented in `server/internal/channels/CONTRACT.md`.
The one-way point to note here: the new server writes only the normalized
tables while the old server reads only the document, so a rollback after
cutover makes anything written since the cutover invisible to the old server.
Those normalized rows remain available on roll-forward, subject to retention;
legacy writes accepted during rollback require explicit reconciliation. Keep
the feature disabled during rollback to avoid divergent writes.

## Acceptance gates

Normalized queries and tenant constraints; migration lint and seeded isolation; concurrency/idempotency/cursor tests; cross-org and private read/write denials; OAuth audience and CSRF isolation; repeated subscription delivery across handler instances and reconnect; live revocation; bounded load and retention tests; frontend regression/type/lint checks; real browser auto-session/reuse/history/scroll checks; native Kit repeated incoming-message and reply proof. Full final diff review and repository-required checks must pass, or remaining failures must be explicitly reported. Existing PoC lint debt belongs to this feature and must be fixed.
