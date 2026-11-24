# Chat transcript encryption (DNO-1573)

## Status: opt-in encrypted shadows, not encrypted-only storage

`server/internal/chat/transcriptcrypto` seals and opens versioned message-body
payloads through the existing organization-scoped KMS/envelope-encryption service.
`server/internal/chat/transcriptstore` persists encrypted shadow copies, verifies
reads, and backfills a bounded project-scoped page at a time. Shared message writers
can capture these copies atomically with the original message and outbox events.
**Only the dashboard chat load (`chat.load`) and chat title generation can read
through shadows, and only for projects on the read allowlist; messages without a shadow fall back to
plaintext. Every other reader still uses legacy data.
Plaintext and existing assets are not removed.** The `chat-transcripts` use case has an independent DEK per
organization; it does not reuse OAuth or gateway-state keys.

The ticket requires per-customer encryption of `content`, `content_raw`,
`tool_calls`, `tool_call_summaries`, `tool_outcome_notes`, and `storage_error`.
The rollout must retain plaintext during a transitional phase, validate a canary
organization first, then expand cutover and destroy the old plaintext. This
shadow foundation is not completion of those acceptance criteria.

The codec covers `content`, `content_raw`, `tool_calls`, `tool_outcome_notes`,
`tool_call_summaries`, and `storage_error`. It binds the project, chat, and message
UUIDs inside the authenticated payload; the underlying encryption service also binds the
organization, use case, and storage column. Opening with another identity or key
fails without returning partial plaintext. Missing keys never fall back to the
application key or plaintext. Key failures retain error classification without
including payloads or provider error text in the codec's error messages.

Ciphertexts bind to `chat_message_transcripts.ciphertext`. The sidecar table is
an expand-only, migration-first dependency, maintained separately from the
application change. Until encrypted-only storage integration exists, transition
reads/mutations reject this use case, and transition listings omit it. Reporting current plaintext transcripts as `platform_key` would be
incorrect: the existing transition runner expects application-key ciphertext.
Provisioning/rotation alone does not encrypt any transcripts.

## Shadow storage and rollout gate

The process flag `--chat-transcript-shadow-project-ids` (environment variable
`GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS`) is empty by default. An explicit
project allowlist is required for process-level activation; there is no implicit
all-project rollout. Startup validates all targets and the schema prerequisite
before provisioning keys. Missing organization encryption or a failed key operation
aborts startup instead of silently disabling encryption. No existing-data backfill
runs automatically at startup.

The shared writer captures a database snapshot in the same transaction as a
message write. This covers Kit projection inserts/patches, native writes, caller-owned transactions, correlated
promotions, imported messages, and complete turns. Shadow failure rolls back the
message and its outbox events. Ordinary observer notifications remain after commit.
Asset uploads can precede the transaction and remain outside this guarantee.
An empty shared-writer project allowlist also disables capture; it never means
all projects.

Capture resolves configuration and keys through the caller's transaction, not a
second pooled connection. Use a writable READ COMMITTED transaction. Its private
TTL cache reuses one DEK within a batch without publishing uncommitted state to
the process cache or starting detached work. This currently costs a fresh KMS
unwrap per Capture batch, even when the process cache is warm; account for that
latency/cost when choosing a shadow allowlist. Keep this opt-in experiment
limited to low-volume projects; broad/high-volume activation is deferred until
a committed-state-safe cross-transaction unwrap cache or an explicit unwrap
rate/concurrency bound is implemented. Reusing transaction-loaded keys globally
is unsafe because those keys or configuration may later roll back.

The unkeyed `source_hash` is not a confidentiality boundary: a reader with access
only to shadow rows can confirm guessed complete payloads and correlate equal
payloads without a DEK. Retaining plaintext elsewhere and default-off capture do
not remove that leakage. Shadow rows must therefore be treated as sensitive
plaintext-equivalent data, not exposed as a ciphertext-only dataset. Replacing or
removing deterministic digests (including existing rows) is a prerequisite for
any encrypted-only rollout, not an optional hardening improvement.

Each copy has a versioned SHA-256 fingerprint of the current sensitive fields,
including JSONB SQL-null flags and nullable `storage_error`. The source fingerprint
version is now `chat-transcript-source/v2`; v1 shadows are stale and must be
recaptured/backfilled even when the current storage error is SQL NULL. The payload
format remains v1: older payloads open with an absent storage error as nil, but
verified reads reject their old source fingerprint. Compare-and-swap persistence
refuses an outdated snapshot.
`Load` checks tenant/project/chat/message linkage, requires a current copy, opens
the authenticated payload, and compares it with the live body. It never substitutes
legacy plaintext after a missing/stale copy or a key failure. The source fingerprint
alone is not proof of authenticity: it is outside the encrypted payload.

`Backfill` processes at most 500 rows per project-scoped page, in message-ID order,
with an atomic page transaction. Resume from the returned cursor; retry a failed
page without advancing. Start another sweep from the zero UUID to pick up concurrent
inserts below a prior cursor. `Progress` distinguishes current copies, waiting/stale
copies, and unattributed legacy rows. **Current shadow counts do not prove plaintext
has been removed or that encrypted-only storage can be finalized.**

Backfill recaptures every scoped source row on each sweep, including hash-current
rows, so forged matching fingerprints or corrupt ciphertext do not prevent repair.
Page counts report rows recaptured, not only missing/stale rows. Progress counts
are fingerprint hints, not ciphertext-authentication or cutover evidence.

Direct SQL updates outside the shared writer can make a shadow stale. Verified
reads reject it, and a subsequent backfill refreshes it. Before encrypted-only
rollout, migrate those writes too and establish a fenced plaintext-removal protocol.

## Shadow reads

`--chat-transcript-shadow-read-project-ids`
(`GRAM_CHAT_TRANSCRIPT_SHADOW_READ_PROJECT_IDS`) is a second allowlist, set on
the server and the worker. Every project on it must also be on the capture
allowlist, or the process refuses to start. For these projects `chat.load` opens each page's message
bodies with `Store.LoadMany` and serves the opened `content`, `content_raw` and
`tool_calls` in place of the plaintext columns.

`LoadMany` leaves out any message that has no shadow at all, and `chat.load`
serves that message's plaintext body. Messages written before capture began, or
by writers that do not seal yet, still load. The read metric counts them under
the `plaintext_fallback` outcome, so coverage gaps stay visible. A shadow that
exists but is stale or fails authentication fails the whole page with an
unexpected error: it means capture or the data is broken, and plaintext must not
hide that. Its codec uses the process's committed data key cache, so a warm page
makes no KMS call, and a page with no shadows makes no key lookup at all.

The worker's `GenerateChatTitle` activity reads the same way. It first decides
from the loaded rows whether the title can change, so a titled chat opens no
shadows. It then opens only the turns it sends to the title model, with the
same plaintext fallback as `chat.load`. A stale shadow fails the activity
without retries, because it stays stale until capture or backfill refreshes it.
A shadow that does not decrypt fails with the same opaque error as a key outage,
so the workflow's normal retries apply. The chat keeps its stand-in title.

Direct SQL writers (see the scope table below) do not seal yet. If one edits a
message that already has a shadow, that shadow goes stale and the read fails
until the message is captured again.

Messages whose body exceeds the inline limit keep it in a raw content asset.
The shadow covers only the inline columns, so `chat.load` still reads those
bodies from the plaintext asset; encrypting assets is tracked under "Raw content
assets" below.

Capture and reads emit `chat.transcript_shadow.capture.*` and
`chat.transcript_shadow.read.*` (duration and message count, by project and
outcome) and `transcriptstore.Capture` / `transcriptstore.LoadMany` spans.

## Scope that still needs implementation

Encrypting a message body is not equivalent to encrypting every transcript copy:

| Surface            | Required integration                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Message writes     | Shared `chat.MessageWrite` paths, transactional inserts, upserts, hook captures, assistant updates, compaction, and direct SQL writers must seal before committing.                                                                                                                                                                                                                                                                             |
| Message reads      | Chat loads, hook reconciliation, assistant history/read-modify-write, background analysis, title generation, and risk evaluation must open before consuming content.                                                                                                                                                                                                                                                                            |
| Raw content assets | Chat capture uploads JSON independently of the inline message fields. Encrypt blob contents and migrate existing objects, including obsolete copies and applicable retention policies.                                                                                                                                                                                                                                                          |
| Attachments        | `chat_content_parts` and the asset service's authenticated/signed attachment downloads need their own encrypted-storage contract.                                                                                                                                                                                                                                                                                                               |
| Derived text       | Chat titles/summaries, resolution/analysis results, risk evidence, and memory digests can contain transcript text. Explicitly include or exclude each store.                                                                                                                                                                                                                                                                                    |
| Metadata           | `origin`, asset URLs, user identifiers, IP addresses, and user agents remain outside this body payload. `storage_error` is sensitive and is included in the encrypted payload, not treated as safe metadata.                                                                                                                                                                                                                                    |
| Hashes             | `content_hash` and the shadow `source_hash` are unkeyed deterministic digests. Equality/guess-confirmation leakage is a known, temporary risk that this shadow-only change does not mitigate. Default-off capture and retained plaintext do not remove it. Before any encrypted-only rollout, remove these digests or replace them with organization-keyed fingerprints, and migrate existing rows; clearing plaintext alone is not sufficient. |

Concrete integration points include:

- `chat/impl.go`: `prepareMessages`, `loadMessageContentFields`;
- `hooks/session_capture.go`, `hooks/claude_transcript.go`, and hook-specific writers;
- `assistants/runtime_kit_transcript*.go`, `assistants/compaction_persist.go`, and
  `assistants/service.go` history loading;
- `background/activities/generate_chat_title.go`, chat-resolution activities, and
  `background/activities/risk_analysis/batch_messages.go`;
- `risk/queries.sql` content batches and `risk/revealmatch.go`;
- `assets/impl.go` attachment uploads/downloads and signed URLs;
- memory-digest inputs and other derived-content readers.

## Rollout requirements

1. Confirm whether the product promise covers message bodies only or all the
   surfaces above, including object storage, derived text, and retained copies.
2. Add encrypted storage with an Atlas-generated expand-contract migration.
   Attribute legacy rows to their owning organization and project first. The
   codec refuses missing project/chat/message UUIDs; do not invent a project or
   infer tenant ownership from an untrusted request. Validate the row's actual
   chat/project/organization linkage before opening.
3. Deploy readers and writers together behind an explicit rollout gate. Keep
   plaintext-to-encrypted migration state distinct from the current
   platform-key-to-organization-key transition model. Define transactional
   fencing, compare-and-swap backfill, retries, and resumable progress before
   removing the transition guard.
4. Backfill **all** in-scope copies, including soft-deleted rows and assets.
   Verify decryption and identity bindings before clearing old representations.
   New writes and read-modify-write operations must not recreate plaintext.
5. Clear plaintext message columns and old blobs according to the approved
   retention policy. Keep authorization and project-scoped access checks; having
   a tenant key is not permission to read every chat in that tenant.
6. Treat unavailable keys as unavailable data, not empty/corrupt history. In
   particular, assistant self-healing must not erase history after a key failure.
   Test cache-bounded suspension, revocation, rotation, and cross-process behavior
   using the existing organization-encryption contracts.
7. Add integration coverage for hooks, assistant streaming updates/compaction,
   risk scans/reveals, background jobs, asset downloads, legacy rows, concurrent
   backfill, and plaintext-copy removal. Enable only after these paths pass.

## Current validation and release blockers

The storage prerequisite is tracked in
[the separate migration PR](https://github.com/speakeasy-api/gram/pull/9418).
Atlas-generated DDL and checksums are available, with composite foreign keys
pinning the shadow to its source message's project and that project's organization.
The existing-table ownership target is built with a concurrent unique index;
review its build cost before deploying to a large message table.

SQLc generation and focused encryption/store/key-service tests pass with that
prerequisite schema overlaid locally, including database rejection of mismatched
message/project/organization ownership. The production schema in this application
change is unchanged. Land and deploy the separate migration before enabling even
the canary; canonical generation/integration tests on the application branch alone
remain dependent on that prerequisite.

Storage-error coverage includes null/empty/value round trips, error-only changes
invalidating freshness, backfill refresh, and rejection of a forged public source
fingerprint when the authenticated storage error differs from the live row.

Startup preflight checks required column types/nullability, defaults for omitted
required columns, and a usable unique `message_id` conflict target before key
provisioning and writer activation. Equivalent deferred conflict arbiters are
rejected. Ordinary readers, encrypted-only writes, cutover fencing, and verified
plaintext destruction remain unimplemented. Do not
interpret shadow coverage or key provisioning as completion of the ticket.

## Product-surface assessment

- **Platform MCP:** no new tool in this foundation. There is no new user action or
  working storage migration to expose. Existing organization key management
  remains the owner of provisioning and rotation; future rollout must ensure
  tools cannot report encryption complete before all in-scope copies are covered.
- **Demo seed:** unchanged deliberately. No dashboard-visible behavior or storage
  format changes here. When storage integration ships, seed both encrypted and
  legacy cases through the supported writer/backfill path and preserve reseed
  isolation/idempotency.
- **Database/audit:** the shadow table requires a separate migration-only change
  before this application change. Message bodies remain unchanged. Key operations
  retain existing organization-encryption auditing; shadow capture is internal
  persistence, not a new user access action.
