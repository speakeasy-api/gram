<!-- Planning snapshot of Linear document 670077e87781, retrieved 2026-09-21 local time. Linear remains the canonical design; this copy travels with the implementation plan. -->

# Gram-native MCP registry and Pulse cutover

## Status and outcome

Reviewed design and agreed planning baseline. Linear has been reconciled into
Stage A, Stage B, Stage C and Completeness with narrative parent/child issues.
This records planning approval, not authorization to start application implementation.

The approved discovery-preview and Goa/SDK amendments have been reconciled to
canonical Linear and the owning tickets. Linear owns the behavioral contract;
this local snapshot accompanies the execution plan, not a separate scope authority.

Gram (`speakeasy-api/gram`) owns the registry in its existing server, Postgres
and staff admin UI. There is no separately maintained/deployed registry repository
or service in the target architecture. This repository's approved snapshot and
schema are migration inputs, not a continuing runtime dependency.

Project: [https://linear.app/speakeasy/project/mcp-registry-45b8963077ab](https://linear.app/speakeasy/project/mcp-registry-45b8963077ab)

This replaces the standalone-service design checkpointed at `4206342` on branch
`checkpoint/registry-cutover-gram-native`. Historical reviews remain evidence of
specific old contracts, not validation of this new database/admin design. The previous Linear plan has now been reconciled: reusable tickets were rewritten,
and obsolete preparatory/hosting tickets retained as superseded history.

## Agreed boundaries

- One global, Speakeasy-managed catalog, not a catalog per organization.
- Gram's own consumers and workers call an internal registry service directly.
  No HTTP requests back to Gram, provider selector, feature flag, shadow reads,
  automatic fallback, or mandatory live dual-adapter release.
- Customer registry reads accept a Gram session cookie OR a valid API key under
  Gram's normal authorization rules. No anonymous read access is introduced.
- Staff admins can read all entries and manage them using the existing staff-admin
  login. Separate admin read routes are fine; ordinary customer sessions/API keys
  do not grant staff access or global-catalog mutation rights.
- A right-side sheet contains a JSON editor with inline JSON Schema errors.
  Explicit Save changes the live entry immediately after server validation.
  No drafts, autosave, review queue, or separate publish step for ordinary edits.
- Unpublish hides an entry from browsing without destroying its data or breaking
  retained reference lookup. Republish makes the same entry browsable again.
- Preserve names, installations, provider identities, and inherited enrichment.
  Import the already-approved snapshot, not a new Pulse export. Keep Figma excluded.
- Recovery is a merged corrective/revert PR followed by a newly built/deployed
  release, potentially restoring Pulse. No configuration-only provider switch or
  instant rollback promise.
- Initial icon images may continue to use the imported Pulse CDN URLs. Removing
  Pulse registry reads does not yet remove this image-hosting dependency. Staff
  uploads of Gram-managed light- and dark-theme icons belong to the separate
  Completeness milestone below, not a prerequisite for the registry cutover.

## 1. Storage and identity

Add one Gram-owned table, provisionally `mcp_registry_entries`:

| Column                     | Purpose                                                                                                     |
| -------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `id`                       | Stable UUID for this entry; generated once, not reassigned on edit/import.                                  |
| `data`                     | JSONB containing the complete registry record, including `server` and retained `_meta` enrichment.          |
| `published`                | Browsing visibility; not a draft state or deletion marker.                                                  |
| `created_at`, `updated_at` | Gram-standard storage timestamps; `updated_at` is also an opaque, lossless stale-editor write precondition. |

Use existing migrations/query tooling. Require nonnull columns and a database
unique constraint/index on the name extracted from `data.server.name`, including
unpublished rows. No separate name column is necessary initially. Validate the
name's presence/type before accepting a write. `id` and `server.name` are immutable
after creation; other fields may change subject to validation and reference guards.

The entry UUID is NOT the legacy Pulse registry/provider UUID. Existing attachments
and Platform provider keys identify a registry namespace plus a server specifier.
Preserve that compatibility identity and retained foreign-key prerequisites; do
not substitute each new entry UUID or rewrite customer installations. Inventory
other live namespaces and unclassifiable references before cutting over; require
an explicit compatible remedy rather than aliasing or silently dropping them.
Retain existing provider tables while references/recovery need them, but stop using
their endpoint/auth metadata to route the new internal registry reads.

## 2. One internal service, two authorized HTTP surfaces

One service owns record lookup, browsing, validation, mutations, and reference
protection. Use Gram's existing Go/SQL conventions rather than retain the separate
Effect server or introduce another generic provider abstraction.

| Caller                       | Authentication/authority                                                            | Behavior                                                                                    |
| ---------------------------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| Customer registry API        | Normal Gram session cookie OR API key with the appropriate existing read permission | Browse/search published entries and resolve records through the documented detail contract. |
| Staff admin API/UI           | Existing staff-admin session                                                        | List/search all entries, read full JSON, create/edit, unpublish/republish.                  |
| Gram server/worker workflows | Existing caller authorization or trusted job boundary                               | Call the service in-process; no loopback HTTP or service API key.                           |

Reuse Gram's existing API design/generation and middleware. Staff admin reads must
not require a customer session or customer project API key. Do not weaken staff
cookie validation or normal API-key scopes; authentication is not authorization.
Use existing cookie-origin/CSRF protections for mutations, and cover direct HTTP
requests that bypass the UI. Existing customer-facing endpoint naming and SDK
wiring remain unchanged. A separate read-only standard adapter is specified in
section 2a; it does not replace these operations or rewire existing consumers.

Browsing excludes unpublished entries and retains existing lifecycle/filtering
behavior. Staff listings expose visibility and validation errors. Detail lookup
can resolve a retained unpublished record for saved references. Unpublishing is a
visibility operation, not revocation, a confidentiality boundary, or a substitute
for deleting an installation. Preserve the current distinction between an absent
record (typed not-found) and an official-deleted record still present in storage:
official deletion hides it from listing, not retained detail lookup. Do not
synthesize official status from Pulse status or vice versa.

Retain current user-facing browsing/search/filter/sort behavior and list/detail
response contracts where consumed by Gram's generated clients. Use bounded,
deterministically ordered pagination for new admin/read operations. Do not build
registry history. The read-only compatibility amendment in section 2a adds a
current-version `latest` selector, not historical-version storage.

Wire every current consumer: dashboard list and both detail/enrichment paths,
Platform search/inspect/register/resume, approval evidence, legacy deployment
processing, registry listing and applicable cache/admin operations. Share parsing
only where semantics match; preserve selected remotes, distinct SSE selection,
header merge/dedup, unknown versus empty tools, and inherited metadata. Stable
provider keys, nonempty compatibility IDs, continuation hashes, and approval
provenance remain unchanged across releases. Lookup failures are unavailable
evidence, not a successful assertion that a server/tool is absent.

### Discovery, retained lookup and coherent evidence

Published discovery is not a prerequisite for resolving a retained reference.
Platform setup handoffs/resume, installed-source authentication enrichment and
existing-server header suggestions must resolve retained entries independently
of browsing. Include these callers in the consumer inventory, not just catalog
pages. Missing enrichment must not become affirmative evidence that authentication
is unnecessary. Known-name lookup must not acquire a new publication-based
prohibition by reusing a browse-list admission check; existing authorization and
other eligibility rules still apply.

URL-based approval evidence lookup includes retained unpublished records, subject
to the same endpoint matching and evidence-eligibility rules as published records.
Unpublish alone neither revokes an installation nor erases its catalog evidence.
Keep publication visibility distinct from official lifecycle status and from
whether evidence is available; do not turn missing or failed lookup into a claim
that authentication or tools are absent.

Each composed inspection or approval result must derive eligibility, selected
endpoint, provenance and tool declarations from one coherent record revision.
Prefer one service-level record read and derive the projections from it; do not
combine listing revision A with detail revision B. No registry history or public
revision API is required. An in-flight lookup may return a wholly earlier result,
a wholly newer result, or an explicit retry/unavailable outcome, but not mixed
revision evidence.

Acceptance: install an OAuth entry, unpublish it, then exercise discovery, setup
continuation, receipt replay, installed authentication/header enrichment and
approval refresh. Discovery hides it while retained resolution and evidence remain
correct. Pause a composed lookup between projection phases and save changed
provenance/tools/remotes; assert coherent evidence rather than a mixed result.

## 2a. Read-only standard registry adapter (approved amendment)

Add a thin HTTP adapter over the same in-process service, not another registry,
database or deployment. Prepare it during Stage A with controlled exposure,
disabled by default until its acceptance gates pass. Existing customer and worker
consumers remain Pulse-backed; enabling this new surface is not the Stage B cutover.
This exposure control is not a registry-provider selector or fallback mechanism.

Define all three registry read operations in Gram's Goa designs, including exact
HTTP routes, query parameters, response envelopes, errors and customer security
requirements. Generate server transports, OpenAPI and dashboard-consumable SDK
operations through the existing repository pipeline. Do not implement a parallel
handwritten-only HTTP contract or dashboard fetch client. Preserve complete
extension metadata through the generated types/serialization. Test encoded server
names and versions through both the generated SDK and actual mounted transport.
SDK availability does not authorize switching existing dashboard reads from Pulse
during Stage A; consumer adoption remains part of the planned cutover.

Contract reference: the generic registry API and OpenAPI at
`modelcontextprotocol/registry` commit
`bf4e88cbe8d1a635c06144ccea1d24cb52fa6186` (OpenAPI version `2025-12-01`):
https://github.com/modelcontextprotocol/registry/blob/bf4e88cbe8d1a635c06144ccea1d24cb52fa6186/docs/reference/api/openapi.yaml
Pin this contract for tests; upgrades require a compatibility review. The old
standalone API is implementation evidence, not proof of standard conformance.

| Read route                                          | Contract                                                                                                                                  |
| --------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /v0.1/servers`                                 | Published catalog with standard `servers`/`metadata` envelope, opaque cursor and bounded limit. `metadata.count` counts the current page. |
| `GET /v0.1/servers/{serverName}/versions`           | Standard list envelope containing the currently retained version only; no invented historical versions.                                   |
| `GET /v0.1/servers/{serverName}/versions/{version}` | Standard record envelope; exact current version or reserved selector `latest`; unavailable versions return typed HTTP 404.                |

No standard publishing/mutation endpoints are added; staff-admin editing remains
the sole global-catalog writer. The nonstandard name-only convenience route is
not required. Server names and versions are URL-encoded path segments; exercise
encoded slashes through the real router, not just service unit tests.

Current-version retention is explicit: changing `server.version` makes the old
version unavailable; edits need not preserve immutable historical bytes. `latest`
selects Gram's currently retained version, not the highest historical semver.
Do not fabricate upstream `isLatest`, lifecycle status or publication timestamps.
Preserve complete extension metadata and keep local catalog timestamps distinct
from upstream metadata. Public-read projection must not reuse retained-installation
lookup in a way that exposes unpublished records through version routes.

Stage A is a **read-only discovery preview, not an incremental synchronization
API**. Implement name-substring `search`, exact/`latest` version filtering and
validated `include_deleted` on all three read routes. Default-exclude inherited
official-deleted records; `include_deleted=true` permits those retained records,
but never overrides local unpublished visibility. Keep internal retained-reference
lookup separate from these discovery rules.

Reject any supplied `updated_since` parameter (including empty or malformed values)
with typed HTTP 400 identifying the unsupported parameter. Do not silently ignore
it. This is an explicit deviation from the full generic read contract; document
the preview limitation rather than claiming full registry or sync compliance.

Use bounded, opaque, immutable-name keyset pagination. A traversal is live, not a
point-in-time snapshot, reliable removal feed or lossless mirror. Concurrent
membership changes may require a new crawl; repeated full reconciliation can
converge after writes settle, not prove point-in-time absence during edits.

No retained version history, edit history, tombstones, change log or speculative
sync columns are added. One mutable record per name remains the storage model.
Full synchronization would require a separately approved consumer/removal,
retention and commit-ordering contract; it is not an unresolved Stage A gate.

**Authentication acceptance gate:** retain normal authorized Gram session-cookie
or API-key reads; no anonymous access or staff privilege escalation. The generic
registry authorization guidance recommends MCP-style OAuth resource-server
behavior. Existing Gram credentials do not by themselves establish generic-client
OAuth interoperability. Document supported credential transport and verify a
representative client before claiming that interoperability; adding OAuth
discovery/challenges is a separately reviewed extension, not implicit scope.

**Routing/exposure gate:** account for the existing `/v0.1/servers` registration in
`configureLocalFixturePlatformMCP` in `server/cmd/gram/platform_mcp.go`; this is a
local-fixture route, not an existing production catalog. Avoid duplicate mounts
and preserve fixture behavior. Verify disabled/enabled exposure, real HTTP auth,
standard envelopes/errors, all three routes, filtering, encoded paths, version
retention, visibility and unchanged Pulse consumers before controlled release.

## 3. Schema validation and immediate-save admin sheet

Bring a single canonical JSON Schema contract into Gram for the complete record.
The editor and server use that same contract with conformance fixtures; do not
maintain independent handwritten validation schemas in Go and TypeScript. Schema
validation is necessary but does not replace uniqueness, immutable-name, or
reference-integrity checks. Preserve extension metadata rather than stripping it.
Port the existing remote-record requirements, required version, and supported URL
and field validation; URL templates remain legitimate data, not verified endpoints.
Do not acquire a runtime dependency on this standalone repository.

Reuse the existing right-side Sheet component. Start with the smallest usable JSON
editor; no field-by-field form builder. List/search shows entry name and visibility.
The sheet loads full JSON and its `updated_at` token, displays syntax/schema errors
with field paths, and offers explicit Save/Cancel. Server validation is authoritative.
Bound payload size while accepting the largest imported record; test real baseline
payloads. Invalid JSON/schema produces a field-level error with no partial write.

New entries save as published. Saving an unpublished entry edits its data without
republishing it; Unpublish and Republish are explicit actions on the same row.
Validate again on republish. No hard-delete endpoint is needed for the first version.
Save and visibility changes reject a stale `updated_at` precondition rather than
silently overwrite another admin's changes. Update timestamps transactionally and
reuse existing staff mutation attribution/audit conventions; no new audit framework.

Treat the `updated_at` precondition as an opaque database-precision token. The API,
generated SDK and editor must preserve and echo it losslessly, without seconds-only
formatting or conversion through JavaScript `Date`. Compare it atomically with the
mutation and ensure every successful Save, Unpublish or Republish advances the
token. Display formatting is separate from the precondition value. Acceptance:
round-trip a fractional-second token through the generated client and save
successfully, then race two Saves and Save versus Unpublish using the same token,
including within one second; exactly one mutation succeeds.

### Structural remote edits and inherited enrichment

Inherited tool/auth enrichment may be keyed by remote array position, such as
`remotes[0]`. Schema-valid JSON and unchanged URL membership do not prove that an
edit preserves the association between an endpoint and its evidence. Import order
preservation alone is insufficient once records are editable.

For the initial editor, reject structural remote edits that would reassign a
populated inherited enrichment slot to a different endpoint or transport. Compare
old and new records; do not infer intent from JSON Schema validation, automatically
transpose evidence, or silently clear it. If structural editing is needed, staff
must first explicitly remove the affected inherited evidence in a separately
validated save; absent evidence remains unknown, not declared-empty tools. Any
subsequent structural save still passes the retained-endpoint guard below. Normal
metadata edits and adding a remote without reassigning existing populated slots
remain available. This is a consistency rule, not a new enrichment pipeline.

Acceptance: reorder or insert into a two-remote record whose endpoints have
distinct inherited tool/auth evidence. Reject reassignment with a useful field-path
error; verify unchanged endpoint/evidence associations after rejection. Explicitly
removing evidence must produce unknown evidence rather than fabricated declarations.

## 4. Protect references when an entry changes

Reject an edit that removes or changes a remote URL still referenced by a retained
installation or deployment. Return a useful staff-only conflict explaining which
URL is in use; do not disclose other organizations' reference data to customer APIs.
Keep normal metadata edits available. This is a narrow reference-integrity guard,
not a claim that every metadata/auth change or upstream service change is harmless.

Cover nondeleted attachments on all deployments that can be cloned/redeployed,
relevant Platform registrations and retained work, not just currently active
installations. Compare selected endpoints using the consumer's eligible-remote
semantics; changing transport so the selected endpoint no longer resolves must
not bypass the guard. Empty/unfiltered selections keep their existing meaning.
For every retained reference, resolve its selection against both old and proposed
records using that consumer's eligible-remote and precedence rules. Reject a change
to the resolved URL or transport, even if all old URLs remain present. This covers
reordering and insertion of a higher-precedence remote, not just URL removal.
Acceptance includes empty and multi-URL filters, reordered streamable-HTTP and
SSE-only arrays, and adding a preferred transport; description-only edits still pass.
Unknown or unclassifiable live references require explicit resolution, not a
one-way-hash decoding assumption.

The check and update must be atomic with reference creation. Use an ordinary
transaction and a shared row-lock discipline for mutations and reference-writing
paths. A writer must revalidate its selected endpoint against the locked current
entry before establishing the protected reference; locking only the eventual
insert does not make an earlier catalog read safe. Define the first durable
protected reference in split-phase receipt/registration work and preserve protection
through completion and retries. Existing continuation/input hashes do not prove
catalog freshness. Do not hold database locks across remote network work or add a
general locking service.

Acceptance: pause installation after inspection but before its first protected
receipt/reference, commit an endpoint-changing edit, then resume. Installation must
re-resolve or reject rather than persist the stale endpoint. In the opposite
ordering, establishing the reference first must make the destructive edit fail.
Cover pending-receipt retry and the legacy attachment-writing paths as well.
The allowance for already-started reads to finish on an earlier result does not
authorize committing a reference that is incompatible with the current entry.

During mixed legacy releases that cannot participate in that discipline, hold
relevant writes or keep edits that could invalidate references unavailable until
convergence. This restriction lasts throughout the Pulse-backed Stage A period and
through Stage B convergence, not merely until all binaries run the Stage A release:
a later Pulse-backed install can still select an endpoint absent from the candidate
record. Ordinary metadata edits remain available. Unpublish keeps the row and
endpoints, so retained lookups work.

## 5. Freshness, publication and security

Postgres is the authoritative live catalog; no startup-only snapshot or restart is
required to publish an admin save. Prefer direct DB-backed reads for this small
catalog over reusing the old upstream Redis cache. Audit all callers so a leftover
Pulse cache cannot serve stale data after the internal-service cutover. Remove or
adapt obsolete cache-clear behavior rather than expose a new generic cache API.
A successful save must be visible to subsequent new reads; already-started requests
may finish using their earlier transaction result.

Refresh affected admin/browser queries after writes and prevent delayed responses
from overwriting a newer edit. At cutover, retire old reads/writers, clear any
retained registry-specific cache prefixes, converge the fleet, then require a full
browser reload before resuming installs. Do not assume closing a dialog retires an
old pending enrichment response. Keep this release transition separate from normal
post-save query refresh.

No registry-to-registry egress credentials, redirect policy, or private-hosting
allowance is needed for internal DB lookup. Existing remote MCP connection/auth
and SSRF protections remain; a stored URL is not an egress authorization. Never
store customer credentials in global entry JSON. Use the existing server's bounded
request handling, errors, logs, and monitoring; no new unauthenticated registry
health endpoint or separate infrastructure decision is required.

## 6. Import and release-based cutover

### Stage A — add storage and administration; Gram stays on Pulse

Ship the additive table, shared validation/service, staff read/write surfaces,
and staff sheet. Existing customer endpoints/workflows still use Pulse in this
release; wire their DB-backed read handlers with the Stage B consumer release,
not ahead of it. Make it clear that admin changes affect the new registry, not live
Pulse. No provider flag is needed: the released consumer code remains Pulse-backed.

Copy the accepted records and schema inputs into Gram. Seed 62 approved records,
excluding the example fixture and Figma, preserving complete payloads, names,
remote order, and inherited metadata. Record input hashes/counts and import outcome.
Make reruns idempotent: insert missing records, detect collisions/conflicts, and
never overwrite subsequent admin edits or mint new IDs for existing names. Use an
explicit import step, not an unconditional boot-time reseed. Known template-based
installation limitations stay documented; this is not recent live verification.

### Stage B — release internal consumers and protect the transition

Adapt all consumers to the in-process service and test on staging. The merged and
deployed replacement release is the cutover, not a separate provider activation.
Compare both imported and subsequently edited DB records with consumer decoding
and projection expectations; the new live database, not an old export alone, is the
candidate source. Test cookie reads, API-key reads, staff reads/writes, and denied
anonymous/customer writes, including unpublished/reference behavior.

Before production, block relevant admin and installation/reference writes and
drain in-flight work while workers still poll. Run the final identity/name/selected-
remote inventory against the exact candidate DB state. Include all cloneable
deployments, registrations, and live pending receipts; classify expiry/provenance
without decoding hashes. Record time and evidence; intervening relevant writes
invalidate the result. Figma or another missing/mismatched record blocks release
until an explicitly approved compatible remedy exists; no silent deletion/alias.

Keep the hold through rollout/convergence, or prove overlap safe. Bound it by all
affected queue/workflow deadlines, including non-registry work; abort/recover if
draining or deployment cannot finish safely. Verify server/worker revision
convergence, fresh internal reads, cache retirement, browser reload and representative
browsing/install/approval/redeploy behavior before resuming work. No application
code may launch listeners or accept work without its necessary persistence/config
readiness. Do not let an unconfigured internal service fall back to Pulse.

### Stage C — observe, recover by new release if necessary, then clean up

In staging, rehearse the replacement release and recovery via a merged corrective
or revert PR and a newly built/deployed release. Preserve additive DB tables/data
through a code reversion. Restoring Pulse must still resolve historical and
replacement-period references, including admin-created entries: do not assume Pulse
has them. If it cannot, use a tested corrective-release remedy instead. Record
secure Pulse prerequisites only where a Pulse recovery release is used. No live
dual adapter, configuration-only toggle, old-artifact downgrade, or automatic fallback.

Reuse Gram monitoring for DB/service read/write failures and latency, validation
conflicts, incomplete catalogs, and registry-backed installation/approval failures.
Exercise alert detection, delivery, recovery and no-data expectations in staging.
Name the production operator/responder, bounded release/recovery procedure and
observation window. Check real outcomes, not just a healthy HTTP process.

Exercise queued/in-flight failures and timeout recovery; a stranded pending
deployment may require a new redeploy rather than retrying the same row. Cleanup
comes only after successful observation and reference/recovery obligations end.
Remove obsolete Pulse readers/credentials and provider-selection/cache machinery
without cascading away retained attachments or compatibility identities. Retire
this standalone repository's runtime role; do not delete its evidence/history here.

## 7. Implementation work and Linear mapping

1. **Store and validate entries:** migration, canonical schema, import and shared
   service, identity compatibility, mutation/reference transaction tests.
2. **Expose authorized registry/admin operations:** customer cookie/API-key reads;
   separate staff read/write operations; visibility/error/concurrency tests.
3. **Build the admin sheet:** list/search, JSON/schema feedback, immediate Save,
   stale-write handling and explicit unpublish/republish.
4. **Move Gram consumers to the internal service:** complete caller coverage,
   preserve wire/projection/identity behavior, retire stale external caches.
5. **Rehearse and ship the cutover:** DB/reference inventory, auth/UI/runtime tests,
   bounded deployment and new-release recovery, monitors and production evidence.
6. **Clean up:** only after retained-reference and recovery obligations end.

Items 1–3 establish Stage A; customer read routing becomes live with item 4 in
Stage B. Item 5 spans rehearsal,
production deployment and observation; item 6 completes Stage C. Storage/schema
contracts unblock parallel API/UI work. Existing ticket IDs can be reused where
helpful, but the old hosting and dual-service adapter tickets must be replaced or
repurposed—not left as hidden prerequisites. The milestone narratives now live in [Stage A / GRW-144](https://linear.app/speakeasy/issue/GRW-144),
[Stage B / GRW-146](https://linear.app/speakeasy/issue/GRW-146),
[Stage C / GRW-145](https://linear.app/speakeasy/issue/GRW-145) and
[Completeness / GRW-150](https://linear.app/speakeasy/issue/GRW-150), with deliverable
child issues. <issue id="41cfd6a7-c3b1-4d78-b74f-4017e7701bf5" href="https://linear.app/speakeasy/issue/GRW-128/make-gram-a-single-registry-consumer-while-retaining-pulse">GRW-128</issue> and <issue id="26f80ae8-df57-4f5c-b384-5a48134dae24" href="https://linear.app/speakeasy/issue/GRW-130/decide-registry-hosting-access-and-operational-ownership">GRW-130</issue> are canceled as superseded, not completed.

## 8. Proposed follow-on milestone — Completeness

### Staff-managed server icon uploads

Add file uploads to the staff-admin entry editor for a server's default icon and
an optional dark-mode override. Light mode is the default. Use the standard MCP
registry icon representation (`server.icons` in the inspected code) for the default
image; do not introduce a competing Speakeasy default/light field. Store only the
optional dark override in a namespaced Speakeasy `_meta` field. Confirm the exact
extension namespace/key and image descriptor shape against the canonical schema
before implementation. Existing imported default icons stay in their standard field.

Staff can upload, preview, replace and remove either image independently. Neither
upload is required to save or publish an entry. The two controls edit the same
in-memory record as the JSON editor, not a separate source of truth. Explicit Save
attaches the staged references using the entry's lossless stale-write precondition.
An upload alone does not change the saved entry. Failure, Cancel or stale Save
must preserve saved state and the user's unsaved edits.

**Display rules**

- Light mode (and consumers without a theme): standard default icon, then the MCP icon.
- Dark mode: Speakeasy dark override, then the standard default icon, then the MCP icon.
- A missing or failed image advances through that order once, without an error loop.
  Light mode never borrows the dark override. Use the MCP icon, not a generic server
  glyph, as the terminal fallback. Missing images never block installation.
- Existing unthemed Pulse icons remain the default image. No conversion of imported
  records or mandatory dark variant is required.

The imported catalog initially retains Pulse CDN URLs. Gram-managed uploads replace
them incrementally; untouched references continue to work as before. There is no
required bulk download, CDN mirroring, new Pulse export or re-enrichment. Removing
Pulse registry reads does not establish independence from Pulse-hosted images while
those URLs remain.

**Formats, storage and security**

Support SVG from v1, alongside PNG and JPEG. SVG is not deferred. Validate actual
contents server-side, not only MIME headers or extensions. For SVG, use a maintained
sanitization approach with an explicit safe subset; reject scripts, event handlers,
external resources, foreign content and other active constructs, and disable XML
external entity/DTD processing. Bound input and parsing complexity. Render sanitized
SVG through image URLs, never inline untrusted markup; serve safe content types with
appropriate response headers. Do not build a custom regex-based sanitizer.

For raster files, inspect dimensions before full decode, then decode/re-encode to
validate and discard incidental metadata. Proposed limits are 1 MiB input and
1024 pixels per raster dimension; define bounded SVG dimensions/viewBox and sanitizer
output limits during implementation design, allowing normal scalable icons. GIF and
WebP uploads are not included in the proposed first version; imported URLs are
unaffected. Exercise valid SVG icons and malicious SVG fixtures before delivery.

Only the existing staff-admin authority may upload or attach global catalog images.
Apply its origin checks and attribution conventions; customer sessions/API keys
must not grant mutation access. These are customer-displayable images, not private
attachments. Reuse Gram asset storage and delivery rather than storing bytes in
JSONB or introducing a separate image service. Save validates managed references as
existing global image assets. Changed bytes get a new asset reference so replacement
does not serve stale bytes at the old URL; identical content may reuse an asset.

Do not delete images on Replace, Remove, Cancel or Unpublish. Retained entries and
shared consumers must keep working. Reclaim abandoned/replaced upload candidates
only after a grace period and a complete reference check, synchronized with new
attachment. Unknown reachability means retain/report, not delete. This includes
non-registry references because global assets may be deduplicated and shared.

### Quick source spike — reusable foundations and implementation outline

Read-only inspection at `4e1fef388a`; no upload prototype, browser test or production
storage verification was performed. Source findings are not proof of an implemented
end-to-end flow.

- `server/internal/assets/adminhandlers.go` already stores platform-tier images in
  the existing assets table/storage with hash deduplication. `assets/urls.go` supplies
  `/rpc/assets.serveImage?id=...` URLs. Reuse the lower-level storage/delivery code.
- The current `adminAssets` endpoint uses ordinary Gram session auth plus `users.admin`
  (`server/design/platformadmin/assets/design.go`), not the separate staff-admin
  cookie. Add a thin route under the staff-admin surface; do not manufacture a
  customer auth context or weaken either auth boundary. Bounded raw-body uploads
  follow the existing pattern; no presigned-upload subsystem is required.
- `sniffMimeType` in `assets/impl.go` only checks supplied MIME strings. It does not
  supply the byte validation, raster decoding or SVG sanitization required here.
- `externalmcp/registryclient.go` currently projects the first standard icon URL as
  `icon_url`. Keep that field as the default for backward compatibility; add a dark
  override projection derived from the Speakeasy metadata. The canonical JSON remains
  the source of truth. Update cards, rows, details and install dialogs to share the
  display rules above, resetting failed-image state when references change.
- The editor labels its controls “Default icon” and “Dark-mode override (optional)”
  and previews light/dark backgrounds. Both controls directly edit their canonical
  fields without discarding unrelated icon descriptors or extension metadata.
- `client/dashboard/src/pages/catalog/useRemoteMcpInstallWorkflow.ts` currently copies
  the catalog image into customer metadata as `logoAssetId`. Preserve that snapshot
  behavior initially using the standard default icon, not the active browser theme.
  Retroactive customer-logo migration, live inheritance and dual-theme installed
  metadata are not implicit scope. Test managed image URLs and SVG through the
  copy-on-install path: the current importer does not support SVG automatically.
  If authorized direct asset association/copy is needed, validate it explicitly;
  do not weaken SSRF checks or silently drop SVG icons during installation.
- Candidate cleanup needs a complete shared-reference inventory. A proposed bounded
  maintenance sweep considers registry-upload candidates older than seven days,
  rechecks under synchronization and deletes only proven-unreferenced assets. Track
  candidate provenance if necessary. Sweep integration is not an existing facility
  established by this spike and must be designed before implementation.

**Implementation sequence and acceptance**

1. Staff upload/storage integration with byte/size/dimension validation and SVG
   sanitization; reject unauthorized requests and malicious images via direct HTTP.
2. Standard default plus namespaced dark metadata in the canonical contract, additive
   projections and Save validation. Test mixed CDN/managed images, lossless unrelated
   metadata round-trip and SVG delivery/copy-on-install compatibility.
3. Two editor controls and shared rendering rules. Test default-only, both variants,
   dark-only, neither, and broken URLs in each theme; the terminal fallback is always
   the MCP icon. Light mode must never use the dark override.
4. Verify upload/replacement, reload, independent removal, Cancel, stale Save and
   cache freshness. Unpublish must not destroy images. Check cleanup's shared-reference
   and attach-versus-reclaim races.

This is the separate **Completeness** milestone ([GRW-150](https://linear.app/speakeasy/issue/GRW-150)), not a prerequisite for
Stages A–C, registry cutover or legacy-provider cleanup. No image browser, remote
URL importer, CDN mirror, separate transformation service or customer-logo migration
is required. The default/dark representation, MCP fallback and SVG-from-v1 scope
reflect the agreed design. Exact extension key, sanitizer choice, remaining limits
and cleanup integration still need implementation design and testing. Application implementation remains a separate execution step. The agreed scope
is now reflected in the milestone hierarchy and project description.

## Evidence and approval boundary

Source anchors were inspected in the frozen Gram snapshot `62f31e48`, not verified
as current production: `server/design/security/{gram_session,api_key,admin}.go`,
`server/design/{externalmcp,admin}`, `client/admin/src/components/ui/sheet.tsx`, and
legacy attachment/query structures. Existing normal registry operations declare
session/API-key alternatives; staff auth is a separate cookie-only session.
Implementation must confirm current source, scope authorization, and deployment
state rather than reuse a stale snapshot blindly.

A subsequent read-only review checked current Gram source at `4e1fef388a`, using
eight specialist reviews (including two Claude Fable 5.1 reviews), a findings
challenge pass and targeted source verification. The amendments above clarify
reference-creation synchronization, retained/unpublished lookup and approval
evidence, endpoint-selection preservation, positional enrichment, coherent reads
and lossless editor preconditions. They are design requirements and acceptance
checks, not claims that the proposed implementation or production state has been
verified. Architecture, delivery scope and the approval boundary remain unchanged.

This design does not claim executable proof of a Postgres registry, mutation guard,
JSON Schema editor, or new authorization surface. Earlier standalone transport and
same-release switching models do not certify them. Run schema/import tests, real
DB transaction/race tests, auth-handler tests and actual browser/service/staging
checks for the new architecture before production. No standalone registry build,
external hosting decision, new import acquisition, drafts, version-history system,
field-form generator, collections, or fresh enrichment pipeline is required.
