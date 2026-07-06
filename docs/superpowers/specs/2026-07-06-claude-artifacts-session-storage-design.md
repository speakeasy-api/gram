# Storing & Sharing Claude Artifacts in Agent Sessions — Design

**Date:** 2026-07-06
**Status:** Exploration / draft for planning

## Problem

Gram ingests Claude.ai and Claude Desktop sessions into `chats` / `chat_messages`
via Anthropic's Compliance API. Those sessions frequently produce **Claude
Artifacts** — the rendered documents Claude generates inline (code, HTML, React,
markdown, SVG, mermaid). Today we store the conversation text but **discard the
artifacts entirely**. Users who review stored sessions cannot see, retrieve, or
share the artifacts that were the actual output of a conversation.

We want to (1) **store** artifacts durably alongside the session that produced
them and (2) **share** them — surface them to authorized viewers and, optionally,
via external links.

## Key finding: the data already reaches us and is dropped

The ingest already receives everything needed — it just throws it away.

- **Ingest path:** `server/internal/aiintegrations/compliance_import.go`
  (`SyncAnthropicCompliance`) — a pull-based Temporal poll, not a push endpoint.
- **The Compliance API client** (`server/internal/thirdparty/anthropic/client.go`)
  decodes three per-message payloads that are currently unused:
  - `Artifacts []ArtifactRef` — `{id, version_id, title, artifact_type}` (client.go:197)
  - `GeneratedFiles []FileRef` — files Claude produced (client.go:176)
  - `Files []FileRef` — user uploads (client.go:175)
- **They are dropped:** `buildExternalMessageRows` (compliance_import.go:338) maps
  only text/tool blocks into `content` / `content_raw`; it never reads
  `msg.Artifacts` / `msg.GeneratedFiles` / `msg.Files`.
- **The download endpoints already exist but have zero callers:**
  `DownloadArtifact(versionID)`, `DownloadGeneratedFile(id)`,
  `DownloadChatFile(id)` (client.go:239–249). Anthropic gives us both the
  references and the content-fetch endpoints.

So this feature is primarily about **capturing data we already fetch**, plus a
thin storage + serving layer built on existing primitives.

## Existing primitives we build on

- **Blob storage:** `assets.BlobStore` (`server/internal/assets/store.go`) — a
  pluggable interface (FS/S3/GCS/Tigris) with content-addressable, deduped
  uploads. Message bodies already offload to
  `{projectID}/chats/{chatID}/{sha256}.json`.
- **`assets` table** (`schema.sql:211`): `kind`-tagged, deduped on
  `(project_id, sha256)`, with kind-specific URL getters and signed-URL serving.
  Backs images, OpenAPI docs, function bundles, and `chat_attachment`s.
- **Signed-URL precedent:** `assets/signed_url.go`
  (`GenerateSignedAssetToken` / `ValidateSignedAssetToken`, HS256, TTL) plus
  `assets.Service.CreateSignedChatAttachmentURL` / `ServeChatAttachmentSigned`
  (impl.go:1262, 1330). This is the only existing "share a blob" mechanism and is
  the model to follow.
- **RBAC:** `chat:read` scope (`authz/scopes.go:39`) — intentionally **not**
  granted to any system role (cross-member transcript reads need an explicit
  custom grant); everyone can read their own sessions via owner-matching
  (`chatVisibilityScope`, chat/impl.go:394). No dedicated asset scope exists.

## Scope

In scope:

- Capture artifact (and optionally generated-file / user-file) references at ingest.
- Download and durably store artifact content in blob storage.
- Surface stored artifacts on the session read API (`chat.loadChat`).
- Serve artifact content to authorized viewers via signed URLs.

Decisions still open (see **Open questions**): payload breadth, capture timing,
and whether external share links are in the first cut.

Explicitly out of scope (separate work):

- **In-browser rendering** of HTML/React artifacts (large security surface — see
  Security). First cut stores + serves raw content for download.
- Backfilling artifacts for sessions imported before this ships.
- Non-Anthropic providers (the compliance API is Anthropic-specific).

## Data model

New table `chat_message_attachments`, a child of `chat_messages`, unifying all
three dropped payloads under one `kind`:

```
chat_message_attachments
  id                  uuid pk (uuidv7)
  project_id          uuid not null  -> projects (cascade)
  chat_id             uuid not null  -> chats (cascade)
  chat_message_id     uuid not null  -> chat_messages (cascade)
  kind                text not null  check in ('artifact','generated_file','user_file')
  external_id         text not null  -- Claude artifact/file id
  external_version_id text           -- artifact version (null for files)
  title               text
  artifact_type       text           -- e.g. text/html, application/vnd.ant.react
  content_type        text
  content_length      bigint
  asset_id            uuid           -> assets (null until content stored)
  storage_error       text
  created_at          timestamptz not null default clock_timestamp()

  unique (project_id, external_version_id) where external_version_id is not null
  unique (project_id, kind, external_id)   where external_version_id is null
```

- **Idempotency:** artifacts are versioned — Claude re-emits a `version_id` per
  edit, and the same artifact `id` can recur across messages. Keying dedup on
  `(project_id, external_version_id)` makes re-imports safe, matching the ingest's
  existing idempotent-upsert ethos. Files (no version) dedup on
  `(project_id, kind, external_id)`.
- **Content** lives in blob storage, registered as an `assets` row with a new
  `kind='chat_artifact'`. Reusing the `assets` table (rather than a bare URL on
  the attachment row, as message bodies do) is deliberate: it gives us
  `(project_id, sha256)` dedup for free and lets the sharing layer reuse the
  existing signed-URL machinery almost verbatim. Path:
  `{projectID}/chats/{chatID}/artifacts/{sha256}.{ext}`.

## Ingest changes

In `compliance_import.go`:

1. **Metadata capture:** after a message page's rows are built/written, upsert one
   `chat_message_attachments` row per `msg.Artifacts` / `GeneratedFiles` / `Files`
   entry (kinds gated by config — see Open questions). This alone is cheap (no
   network) and immediately enables "this turn produced artifact X (title, type)".
2. **Content capture:** for each new attachment, call the matching `Download*`
   method, hash + upload via `BlobStore.Write`, create the `assets` row
   (`kind='chat_artifact'`), and set `asset_id`. Bound the size (reuse the
   `maxAssetReadSize` = 20 MiB ceiling) and, on failure, record `storage_error`
   without failing the whole import — mirroring how message-content storage
   already degrades.
3. **Pipeline placement:** downloads are I/O-heavy and per-artifact. Run them in a
   dedicated stage (a 4th stage after the message writer, or a bounded worker pool
   fed by the writer) so artifact downloads never block message writes or the
   per-chat cursor advance.

## Read / share changes

1. **Internal read:** extend `chat.loadChat`'s `ChatMessage`
   (`server/design/chat/design.go:387`) with an `artifacts[]` array — metadata
   plus a serve URL — gated by the existing `chat:read` + owner-matching. This is
   "sharing" in the sense of surfacing artifacts to authorized org members.
2. **Serve content:** add an artifact serve endpoint reusing
   `GenerateSignedAssetToken` / `ServeChatAttachmentSigned` (generalize the
   attachment signer to accept `kind='chat_artifact'`, or add a sibling method).
3. **External share links (optional, phase 2):** `chat.createArtifactShareLink`
   mints a TTL-bound JWT for a single artifact that bypasses session auth — the
   real "send it to someone outside the org" story. Every share mint should be
   audit-logged (see Security).

## Security

- **Rendering danger:** HTML/React artifacts are arbitrary JS. If we ever render
  them (not just download), they MUST be served from a sandboxed origin distinct
  from the dashboard, with a strict CSP, or we introduce stored-XSS /
  data-exfiltration. First cut avoids this by serving raw content with
  `Content-Disposition: attachment` (or a non-executable content type). In-browser
  rendering is explicitly a later, separately-designed step.
- **Sensitivity:** artifacts may contain the most sensitive output of a session.
  Internal access stays behind `chat:read` + owner-matching. External share-link
  creation and consumption should be recorded via the `gram-audit-logging`
  subsystem (actor / action / subject), like other sensitive operations.
- **Size / cost:** eager download adds one Compliance-API call per artifact
  version. Consider a per-`ai_integration_config` toggle (`import_artifacts`) so
  orgs can opt out, and enforce the size ceiling.

## Delivery (phased)

- **Phase 1 — metadata capture:** schema (migration) + ingest wiring for
  attachment references only. No downloads, no API surface. Unblocks "artifacts
  existed here" in the data.
- **Phase 2 — content storage:** download + blob-store content, `assets` rows,
  `asset_id` linkage. Config toggle + size ceiling.
- **Phase 3 — internal sharing:** `loadChat` artifacts array + signed serve
  endpoint.
- **Phase 4 — external share links:** tokenized links + audit logging.

Each phase is independently shippable and useful.

## Testing / verification

- `mise build:server` compiles after the Goa design + schema changes.
- Migration generated via the `postgresql` skill workflow (schema.sql → migration
  → `atlas.sum`); `mise gen:sqlc-server` for the new queries.
- Ingest unit tests: a compliance message page with `artifacts` / `generated_files`
  produces attachment rows; re-import is idempotent (no duplicate rows); a failed
  download records `storage_error` without failing the page.
- Read tests: `loadChat` returns artifacts for the owner; a non-owner without
  `chat:read` does not.
- `mise gen:sdk` + dashboard `tsc` if the read type changes.

## Open questions

1. **Payload breadth** — artifacts only, artifacts + generated files, or all three
   (incl. user uploads)? The table and download machinery handle all three; the
   difference is which `kind`s the ingest populates. *Recommendation: artifacts +
   generated files.*
2. **Capture timing** — eager download at ingest (durable even if the Claude
   source is later deleted — the point of a compliance product; costs API calls),
   vs. metadata-now + lazy content on first view. *Recommendation: eager, gated by
   a config toggle.*
3. **External share links** — in the first cut, or internal viewing only to start?
   *Recommendation: internal first, external links as Phase 4.*
4. **Config toggle** — should artifact import be opt-in per `ai_integration_config`,
   or on by default for the Anthropic compliance provider?
