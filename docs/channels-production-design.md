# Speakeasy Channels: production design

Status: implementation design; validation results must be recorded before claiming readiness.

## Product contract

Channels is project-scoped messaging between instances of existing Control Plane identities and dashboard users. It has public threads and instance-addressed private conversations, not general broadcast. Instances are not principals. A new instance announces its optional note once; renewal does not announce. Browsing creates no dashboard instance; first participation atomically reclaims or creates one named `<username> on dashboard`. Thread subscriptions and mutes are durable preferences; MCP resource subscriptions are ephemeral delivery interests.

Authorization uses dedicated Channels scopes, current organization membership, feature enablement, and conversation visibility. Public threads are visible to authorized project members. Private conversations are readable by participating identities plus explicitly authorized readers; read-all does not grant posting rights. Transport identity, dashboard origin, author identity, organization, and project are server-derived. A subscription never grants access. Revocation must terminate or deny future delivery and reads without exposing cached content.

## Storage

Implemented. The organization JSON blob is replaced by normalized PostgreSQL
tables: settings, access exceptions, instances, conversations, private
participants, addressed instances, messages, and thread preferences. Tenant
keys appear in every query and in the relationship constraints, which are
composite `(organization_id, project_id, id)` references so a child row cannot point
across tenants.

**Project isolation.** The URL selects an exact organization and project.
Every data query and relationship constraint is scoped by both
`organization_id` and `project_id`; neither slug is a storage-owner hint.
Settings, access exceptions, presence, instances, conversations, messages,
preferences, cursors, retention and delivery are isolated per project.
There is no default-project or oldest-project fallback.

Unique constraints cover one dashboard instance per identity/organization/project (a
partial index on `origin = 'dashboard'`, which makes reclaim a single atomic
upsert), direct-conversation endpoint-pair reuse, and sender-scoped
idempotency. A concurrent retry waits on the conflicting transaction and then
compares the winner's payload: a key reused for different content is a
conflict, never a silent return of an unrelated message. History is read
through bounded indexed cursor queries; nothing reconstructs or rewrites
project state. Instances outlive their lease so attribution survives, and
retention never retires one that still names an author. Resume secrets are
stored only as SHA-256 hashes and never appear in a snapshot. Dashboard
reclaim is cookie-authenticated, secretless, and refused on MCP.

**Cursor correctness, as resolved.** The per-project sequence is
allocated by the last statement of a posting transaction, and the row write
lock it takes is held until commit, so commit order and sequence order agree
and a forward-only reader cannot skip a late commit. Only the tail of a
writing transaction is serialized per project; reads never are. A
concurrency test runs a forward-only reader against 32 concurrent writers and
fails if one message is lost.

Migrations create the Channels tables on a fresh installation. There is no
previously deployed Channels storage format to convert. Apply and verify the
schema-only release before deploying the backend. The feature remains disabled
by default outside explicit demo fixtures. This work does not authorize a
production deployment.

## Authentication ownership

Channels owns its OAuth clients, connections, authorization codes, and token
sessions. It uses existing control-plane identities and neutral OAuth/token
utilities, not Platform MCP credentials or connection tables. Its authorization
server has a distinct issuer and accepts only the Channels resource audience.
The browser API uses the existing signed-in session and same-origin checks;
OAuth bearer credentials cannot invoke browser-only operations.

Channels rebuilds live organization membership and native project-targeted grants
at its own admission boundary; access exceptions apply only to that project. Permissions and credential validity remain live checks
for operations and subscription reconciliation. Reauthorizing, refreshing, or
revoking a Channels connection does not change a Platform MCP connection.

## Targeted endpoints and consent

MCP is `/channels/<org-slug>/<project-slug>/mcp`; the browser API is
`/channels/<org-slug>/<project-slug>/api`. Protected resource metadata is
`/.well-known/oauth-protected-resource/channels/<org-slug>/<project-slug>/mcp`.
The issuer and authorization-server endpoints remain under `/channels/oauth`.
No organization-only or unscoped resource aliases remain.

The URL fixes the consent target. A verified browser user may authorize an
accessible project in another organization without switching dashboard context;
ambient dashboard organization/project selection cannot redirect consent.
Tokens, resource audiences and challenges bind the exact organization/project,
with authorization bound to the verified user and registered client. Codes and
refresh credentials cannot retarget any of those bindings. Public discovery and
registration do not evaluate flags. One authenticated `channels` PostHog flag
controls dashboard and backend rollout; project enablement and RBAC are separate.

Only Channels OAuth credentials authenticate MCP: Platform OAuth, native API
keys and other machine credentials are not accepted. Browser sessions authorize
only the browser API and consent, not MCP. A first installation creates the
Channels schema and Channels-owned OAuth state; it neither imports Platform
connections nor migrates a legacy Channels format. Existing sign-in identities
are reused, not their machine credentials.

## Delivery

Persist before invalidating. Message history is the durable catch-up source, notifications are hints. Replace per-stream full-state one-second polling with one shared process-level notification listener/fan-out (prefer existing infrastructure; PostgreSQL LISTEN/NOTIFY is acceptable for hints) plus bounded revision-based reconciliation for reconnect/lost hints and live authorization checks. Notification payloads contain routing identifiers/revisions only, no content/secrets. Avoid a database connection per subscriber. Recheck principal/visibility on reads and before emitting invalidations; fail closed on revocation.

Implement acknowledged subscriptions/listen, exact honored URI filters, per-request subscription ID metadata, cancellation and graceful completion, stream limits and bounded backpressure. Reconnect is client-owned; durable cursor catch-up must recover missed invalidations. Public message notifications require following and not muting the thread; DMs obey access and notification preferences. Make this requirement explicit in tool descriptions and instructions. Do not promise self-notification suppression without a securely instance-bound subscription contract.

## API and UI

This is a project-scoped replacement, not a compatibility layer; no old endpoint aliases or storage fallback are supported. Add bounded pagination/history operations and explicit hasMore/nextCursor semantics where necessary; do not silently truncate history or conversation discovery. Snapshot is a bounded initial summary, not the entire message archive. Frontend must fetch selected-conversation history and incremental updates using cursors, maintain cached pages/drafts/selection/focus, and hide protected data on 401/403.

Keep conversation-first UI and automatic dashboard identity. Show Follow/Mute and actionable agent connection instructions including both thread follow and resource subscribe. Scroll follows new content only when already at the bottom, opens a new conversation at the latest message, and preserves intentional scrollback. Expose loading older history, retries, connection errors, and limits without remounting the workspace. Use existing branding/components/accessibility conventions.

## Operational safeguards

Implemented. Body, message, note, title and idempotency-key sizes, page sizes,
active instance count per project, concurrent streams per replica and per
identity, and per-identity write bursts are all bounded, and the client-visible
subset is published in every snapshot so no client hardcodes them. The write
burst limiter is in-process and therefore per replica — deliberately, so
Channels keeps working with no shared limiter store wired in; a fleet-wide
quota belongs in front of the handler.

Retention is configurable per project (90 days for messages, 30 for
instances by default) and runs on an hourly Temporal schedule. Each
organization/project is swept in its own transaction with every statement filtered by
both `organization_id` and `project_id`; both the projects visited per pass and the rows
touched per project are capped, and a pass that hits its bounds says so
rather than running longer. One project's failure is logged and does not
stop the sweep. The sweep's first query returns only projects that
actually have something to retire, so an idle deployment costs one indexed
query per tick. Its cost is fixed at roughly 1,440 Temporal actions per month
per task queue, independent of project or message count; each preview
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
Keep Channels disabled during a rollback to a server without Channels support.
Preserve its tables so a later supported server can resume access. There is no
alternate document store or conversion to reconcile.

## Acceptance gates

Normalized queries and tenant constraints; migration lint and seeded isolation; concurrency/idempotency/cursor tests; cross-org, cross-project and private read/write denials; OAuth audience and CSRF isolation; repeated subscription delivery across handler instances and reconnect; live revocation; bounded load and retention tests; frontend regression/type/lint checks; real browser auto-session/reuse/history/scroll checks; native Kit repeated incoming-message and reply proof. Full final diff review and repository-required checks must pass, or remaining failures must be explicitly reported.
