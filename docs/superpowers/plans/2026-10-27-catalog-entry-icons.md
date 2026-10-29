# Catalog Entry Icons Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let staff upload a primary SVG logo and optional dark-mode SVG in Admin, safely host the files, and publish their metadata on catalog-entry envelopes.

**Architecture:** Reuse platform assets and the existing registry JSON document/save flow. Store curated icon objects under envelope `_meta["com.speakeasy.ai/icons"]`, never modify upstream `server.icons`, and use the existing public `/rpc/assets.serveImage` route. Validate and reserialize a deliberately small static SVG subset using `encoding/xml`; do not add a sanitizer dependency, CDN, table, or separate upload service.

**Tech Stack:** Go standard library, existing Goa/SQLc/PostgreSQL services, platform blob storage, React/TypeScript, Vitest/Testing Library, staff MCP proposal framework.

**Spec:** `docs/superpowers/specs/2026-10-27-catalog-entry-icons-design.md`

## Global Constraints

- Customer-dashboard icon rendering and selection are explicitly out of scope.
- Preserve upstream `server.icons` unchanged.
- There are two slots: `default` for the primary logo and `dark` for an optional dark-background alternative.
- The slot key is authoritative; icon objects do not contain a redundant `theme` field.
- Reject unsupported features rather than silently stripping them.
- SVG-only applies to new curated uploads, not upstream icons.
- Public reads must not require staff credentials; writes must require platform-wide staff authorization.
- No new garbage-collection subsystem is part of this slice.
- No new dashboard page or navigation is planned.
- Do not hand-edit generated Goa, SQLc, SDK, or migration outputs. No schema migration is expected.
- Follow repository naming/confidentiality rules. Never bypass review, approval, or checks.

## Repository findings and boundaries

- `assets.StorePlatformImage` already hashes/stores platform assets. Its raster allowlist currently excludes SVG; the staff token-upload handler has a second raster-only check. Both must route SVG through the same validator.
- `assets.ServeImage` streams storage bytes through the API, rather than redirecting to a bucket. Add protections to this actual response, not the Admin HTML response.
- The existing image limit is `MaxFileSizeImage = 4 * mib`; reuse it.
- Registry entries already store the complete envelope in `mcp_registry_entries.data`. `Save` locks the row and checks an opaque revision token. Do not add a second curated-icon table/column or a second form-save endpoint.
- `NativeRegistryReader` already preserves arbitrary `_meta` on list/detail conversion. Prove that with regression tests before changing its implementation.
- The inspected tree has a startup Okta seed/merge, not a general upstream registry replacement job. Protect curated metadata through that existing merge. Do not invent a sync worker. Any future upstream importer must preserve the staff namespace and must not adopt an upstream value for it.
- Staff MCP already has single-use image upload tokens and an approved registry-create proposal. Reuse upload capability; add a narrow approved icon-update proposal for existing entries, not arbitrary JSON editing.
- Option C screenshot is the visual reference. The written spec adds remove/error/loading behavior omitted from that static mockup.

## File map

| Responsibility                    | Files                                                                                                                                                                       |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Static SVG policy                 | New `server/internal/assets/svg.go`, `svg_test.go`                                                                                                                          |
| Upload/serve integration          | `server/internal/assets/adminhandlers.go`, `platform_image_uploads.go`, `impl.go`, `urls.go`; existing upload/serve tests; `server/design/assets/design.go`                 |
| Registry icon contract/validation | New `server/internal/mcpregistry/icons.go`, `icons_test.go`; `contract/speakeasy-registry.schema.json`, contract tests, `mutations.go`, `service.go`, `queries.sql`         |
| Catalog response/merge coverage   | `server/internal/mcpregistry/http_test.go`, `serialization_regression_test.go`, `oktaseed/oktaseed_test.go`; `server/internal/externalmcp/native_test.go`                   |
| Editor                            | New `client/admin/src/pages/registry/RegistryIcons.tsx`, `RegistryIcons.test.tsx`, `registryIcons.ts`, `registryIcons.test.ts`; existing `RegistryEntrySheet.tsx` and tests |
| Staff agent parity                | New `server/internal/adminmcp/tool_write_registry_icons.go` and test; `tool_image_upload.go`, `tool_registry.go`, write registration/guard and contract tests               |
| Fixtures                          | `server/internal/demoseed/local.go`, `local_test.go`; new tiny static SVG under `server/internal/demoseed/fixtures/`                                                        |

## Execution order

Tasks 1 → 2 → 3; Tasks 4 and 5 consume Task 3; Task 6 verifies the complete slice. Each task has its own red/green cycle and reviewable commit. Use existing worktree; verify isolation before executing. Load relevant skills: ponytail, test-driven-development, postgresql for SQL, maintaining-admin-mcp, maintaining-platform-mcp, vercel-react-best-practices, gram-demo-seed, gram-playwright-cli, verification-before-completion. No Command K changes: no dashboard routes or searchable resources change.

### Task 1: Strict static SVG validation and canonical serialization

**Files:** Create `server/internal/assets/svg.go`, `svg_test.go`.

**Interface:** `func sanitizeSVG(data []byte) ([]byte, error)` in package `assets`. It accepts complete bytes, returns only canonical allowed SVG or an error, never partially sanitized output. Task 2 hashes/stores the returned bytes.

- [ ] Write table tests before implementation. Include this smoke test and table cases listed below:

```go
func TestSanitizeSVG(t *testing.T) {
    good := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#8b5cf6" d="M0 0 L24 24"/></svg>`)
    clean, err := sanitizeSVG(good)
    require.NoError(t, err)
    again, err := sanitizeSVG(clean)
    require.NoError(t, err)
    require.Equal(t, clean, again)
    _, err = sanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`))
    require.Error(t, err)
}
```

- [ ] Run `mise run test:server ./internal/assets -run TestSanitizeSVG -count=1`; expect undefined-function failure.
- [ ] Implement using `xml.Decoder` in strict mode, no custom entities/charset reader, bounded input and output (`MaxFileSizeImage`), maximum depth 64 and maximum 100,000 tokens. Track one root and reject multiple roots, non-whitespace outside root, directives/DOCTYPE, unknown namespaces and duplicate attributes. Permit an optional leading XML declaration only with version 1.0 and UTF-8 encoding; drop comments deterministically. Never fetch resources.

Use these policy constants and decoder settings in `svg.go`:

```go
const (
    svgNamespace = "http://www.w3.org/2000/svg"
    maxSVGDepth = 64
    maxSVGTokens = 100_000
)
var svgElements = map[string]bool{
    "svg": true, "g": true, "path": true, "rect": true,
    "circle": true, "ellipse": true, "line": true,
    "polyline": true, "polygon": true, "title": true, "desc": true,
}
```

```go
// Inside sanitizeSVG, after checking len(data) against MaxFileSizeImage:
decoder := xml.NewDecoder(bytes.NewReader(data))
decoder.Strict = true
var output bytes.Buffer
encoder := xml.NewEncoder(&output)
```

Read tokens until EOF. Validate each token against the closed policy below before passing it to `encoder.EncodeToken`; reject with nil output on any violation. Normalize namespace declarations before emission so output round-trips without duplicate `xmlns` attributes. Require exactly one completed root and flush the encoder only after successful EOF validation. Check encoded size before returning output. Closed policy:

| Item                    | Accepted values                                                                                                                                                                |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Elements                | `svg` (root only), `g`, `path`, `rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`, `title`, `desc`                                                                    |
| Root namespace          | Exactly `http://www.w3.org/2000/svg`; emit one canonical root declaration                                                                                                      |
| Geometry attributes     | Element-appropriate `d`, `points`, `x`, `y`, `x1`, `y1`, `x2`, `y2`, `cx`, `cy`, `r`, `rx`, `ry`, `width`, `height`, `viewBox`                                                 |
| Presentation attributes | `fill`, `stroke`, `stroke-width`, `fill-rule`, `clip-rule`, `opacity`, `fill-opacity`, `stroke-opacity`, `stroke-linecap`, `stroke-linejoin`, `stroke-miterlimit`, `transform` |
| Paint                   | `none`, `currentColor`, `black`, `white`, `transparent`, or hexadecimal 3/4/6/8-digit color; reject all URL/functions/variables                                                |
| Numeric geometry        | Finite numbers; optional `px` only for lengths; positive root dimensions/radii where applicable; nonnegative sizes/stroke widths; opacity in [0,1]                             |
| `viewBox`               | Four finite numbers; positive third/fourth values                                                                                                                              |
| Paths/points            | SVG path command characters and finite numeric token syntax only; points are numeric pairs; no alphabetic values beyond path commands/exponents                                |
| Transform               | Parse a list of `matrix`, `translate`, `scale`, `rotate`, `skewX`, `skewY`, with their SVG argument counts and finite numeric values; reject anything else                     |
| Text content            | Only `title`/`desc` text and inter-element whitespace; encoder escapes text                                                                                                    |
| Enumerated attributes   | Explicit SVG enums for linecap/linejoin/fill-rule/clip-rule; never arbitrary strings                                                                                           |

Reject `style`, `class`, `id`, `href`, any event attribute, namespaced attributes, `foreignObject`, `script`, `use`, `image`, `a`, animation, text/fonts, filters and gradients. No regex substitution of markup. Reject unsupported files with "Upload a static SVG using paths and basic shapes; styles, links, and embedded content are not supported."

- [ ] Add malformed XML, foreign namespace tricks, duplicate attributes, entity/DOCTYPE, processing instruction, CSS URL, script/event, javascript/data URLs, embedded HTML, external references, oversized/deep/token-heavy input cases. Check no output on errors, stable output, valid namespace, and preservation of accepted shape attributes. Add fuzz test `FuzzSanitizeSVG` for parse/re-serialize idempotence and no panic.
- [ ] Run the focused tests and `mise run test:server ./internal/assets -run '^$' -fuzz FuzzSanitizeSVG -fuzztime 10s`. Inspect valid flat-path assets in a browser during Task 6; do not claim visual fidelity for rejected features.
- [ ] Commit `feat: validate static SVG platform images` after security-focused review of the allowlist.

### Task 2: Integrate SVG uploads and safe public serving

**Files:** Modify `server/internal/assets/adminhandlers.go`, `platform_image_uploads.go`, `impl.go`, `urls.go`, `server/design/assets/design.go`; extend `uploadplatformimage_test.go`, `platform_image_uploads_test.go`, `serveimage_test.go`. Generated outputs follow Goa generation.

**Consumes:** `sanitizeSVG([]byte) ([]byte, error)`.
**Produces:** Existing admin upload endpoint returns an asset with canonical SVG bytes/content type/hash; existing staff upload-token endpoint returns its existing `StoredPlatformImage` shape and canonical public URL. No new endpoint. `ServeImage` sends protective headers.

- [ ] Extend existing authorized upload fixtures with a valid SVG. Assert returned type `image/svg+xml`, size/hash equal canonical bytes, repeat upload deduplicates, and same input through both upload entry points produces the same stored asset. Keep existing raster tests green.
- [ ] Add hostile SVG, forged MIME, empty file, declared-size mismatch, 4 MiB + 1 byte, expired/reused token and non-staff cases. Reuse the existing SVG-with-script test; safe SVG acceptance is a separate case, not deletion of the security regression.
- [ ] Run `mise run test:server ./internal/assets -run 'Test.*(PlatformImage|ServeImage)' -count=1`; new safe-SVG tests must fail before implementation.
- [ ] In the shared storage path, detect/parse SVG bytes before hashing/dedup/storage. Do not use `http.DetectContentType` alone (SVG often sniffs as XML/text). Route XML candidates through `sanitizeSVG`; reject them on failure. Explicit SVG MIME with non-SVG bytes is rejected. Raster requests retain existing handling. Use the transformed byte length/type in upload results, including staff token results, rather than original upload values.
- [ ] Include SVG in `PlatformImageTypes` only once both shared validation and response protections are wired. Keep project/org upload SVG rejection unchanged; this slice extends staff platform uploads, not all upload callers.
- [ ] Add Goa response attributes and HTTP header mappings, then fill them in `ServeImage`:

```text
Content-Type: image/svg+xml
Content-Security-Policy: default-src 'none'; script-src 'none'; style-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; sandbox
X-Content-Type-Options: nosniff
```

Apply CSP for SVG and nosniff for image responses. Retain existing CORS/CORP behavior. No redirect or bucket URL. UUID/content-dedup asset URLs already change when bytes change; do not add cache invalidation or immutable caching to unrelated routes.

- [ ] Test the mounted generated HTTP handler, not only `ServeImage` return fields. Fetch without auth; assert headers, SVG MIME, body, and no redirect. Confirm existing PNG/JPEG public serving remains valid.
- [ ] Run `mise run gen:goa-server`, then focused asset tests. Run `mise run gen:sdk` if generated public response contracts require SDK regeneration; inspect for unrelated churn and never hand-edit generated files.
- [ ] Commit `feat: serve validated SVG assets with restrictive headers`.

### Task 3: Persist and validate the catalog icon namespace

**Files:** New `server/internal/mcpregistry/icons.go`, `icons_test.go`; modify `contract/speakeasy-registry.schema.json`, `contract/speakeasy_registry_test.go`, `service.go`, `mutations.go`, `queries.sql`; extend native response and Okta seed tests named in the file map. Update registry construction sites in `server/cmd/gram/{admin,start,worker,startup_seeds}.go`, `server/internal/background/worker.go`, and test setup only as needed to supply the public asset base URL.

**Interfaces:**

```go
const IconsNamespace = "com.speakeasy.ai/icons"
type CatalogIcon struct {
    Src string `json:"src"`
    MimeType string `json:"mimeType"`
    Sizes []string `json:"sizes"`
}
type CatalogIcons struct {
    Default *CatalogIcon `json:"default,omitempty"`
    Dark *CatalogIcon `json:"dark,omitempty"`
}
func ParseCatalogIcons(data json.RawMessage) (CatalogIcons, error)
func WithCatalogIcons(data json.RawMessage, icons CatalogIcons) (json.RawMessage, error)
func (s *Service) WithAssetBaseURL(base *url.URL) *Service
```

`WithAssetBaseURL` copies configuration at startup, not per request. A missing base URL must reject nonempty curated icon writes, never accept arbitrary hosts. Existing records without this namespace remain unaffected. `WithCatalogIcons` changes only this namespace and removes it when both slots are nil; use `map[string]json.RawMessage` to avoid rounding unrelated numbers. An independent dark slot may remain after primary removal; show no curated primary in that state, never silently delete the alternative.

- [ ] Write pure helper tests with an upstream icon and unrelated metadata containing an integer > 2^53. Assert adding/removing either slot preserves both and removing the last slot removes only this namespace.

```go
func TestCatalogIconsRoundTrip(t *testing.T) {
    raw := json.RawMessage(`{"server":{"icons":[{"src":"https://upstream.example/icon.png"}]},"_meta":{"example.test/data":{"n":9007199254740993}}}`)
    icons := CatalogIcons{Default: &CatalogIcon{Src: "https://assets.example/rpc/assets.serveImage?id=00000000-0000-4000-8000-000000000001", MimeType: "image/svg+xml", Sizes: []string{"any"}}}
    updated, err := WithCatalogIcons(raw, icons)
    require.NoError(t, err)
    got, err := ParseCatalogIcons(updated)
    require.NoError(t, err)
    require.Equal(t, icons, got)
    cleared, err := WithCatalogIcons(updated, CatalogIcons{})
    require.NoError(t, err)
    require.JSONEq(t, string(raw), string(cleared))
}
```

- [ ] Run `mise run test:server ./internal/mcpregistry/... -run 'Test.*(CatalogIcons|Speakeasy)' -count=1`; expect missing helpers/schema checks to fail.
- [ ] Add a closed namespace schema: only `default` and `dark`; each object requires `src`, `mimeType` fixed to `image/svg+xml`, `sizes` fixed to `["any"]`; no `theme` or additional fields. Validate root placement and URL lengths using existing contract conventions. Canonicalize an empty namespace away on save. The existing Monaco overlay import picks up the same schema; no duplicate browser schema.
- [ ] Add a SQLc lookup for association validation in `mcpregistry/queries.sql`:

```sql
-- name: IsCatalogSVGAsset :one
SELECT EXISTS (
    SELECT 1 FROM assets
    WHERE id = @id AND project_id IS NULL AND organization_id IS NULL
      AND kind = 'image' AND content_type = 'image/svg+xml' AND deleted IS FALSE
);
```

Parse `src` with `net/url`, require exact configured public asset origin/path, exactly one `id` query parameter containing a nonzero UUID, no userinfo/fragment/additional query keys, and canonical URL equality with `assets.ServeImageURL`. Permit HTTP only when the configured development base itself uses HTTP; production URLs must be HTTPS. Validate both slots inside the existing create/save transaction using the lookup. No network fetch, arbitrary SVG URL, tenant asset, deleted asset, or raster association is accepted. Keep errors scoped to the icon field without leaking another tenant's data.

- [ ] Canonicalize icon metadata before stored-record byte admission and persist it in the existing JSON field. Apply the same validation to `CreateTx`, `Save`, and later MCP transactional updates. Do not change publication metadata, revision checks, or endpoint immutability.
- [ ] Run `mise run gen:sqlc-server`; add integration tests for platform versus tenant/deleted/non-SVG assets, spoofed origins, conflict tokens, failed saves preserving prior icons, and unchanged publication/endpoint rules.
- [ ] Add regression cases to `oktaseed` proving its current merge preserves the namespace and upstream icons. Add raw registry list/detail and `externalmcp/native_test.go` tests proving the namespace is returned unchanged. Keep summary-only admin lists summary-only; they are not full catalog envelopes. No projected API change unless the test proves a loss.
- [ ] Run `mise run test:server ./internal/mcpregistry/... ./internal/externalmcp -count=1`. Commit `feat: add curated catalog icon metadata`.

### Task 4: Compact Admin controls sharing the JSON draft

**Files:** Create `registryIcons.ts`, `registryIcons.test.ts`, `RegistryIcons.tsx`, `RegistryIcons.test.tsx` under `client/admin/src/pages/registry/`; modify `RegistryEntrySheet.tsx`, `RegistryEntrySheet.test.tsx`.

**Interfaces:**

```ts
export type CatalogIcon = {
  src: string;
  mimeType: "image/svg+xml";
  sizes: ["any"];
};
export type CatalogIcons = { default?: CatalogIcon; dark?: CatalogIcon };
export type IconSlot = keyof CatalogIcons;
export function readCatalogIcons(text: string): CatalogIcons;
export function setCatalogIcon(
  text: string,
  slot: IconSlot,
  icon: CatalogIcon | null,
): string;
export type RegistryIconsProps = {
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
  onBusyChange: (busy: boolean) => void;
};
```

Use the installed `jsonc-parser` edits to preserve unrelated raw text/numbers. Invalid JSON must disable icon writes with a repair message, not silently replace the document. Keep the JSON draft authoritative; no separately saved icon form state.

- [ ] Write helper tests: default/dark add/replace/remove, empty namespace cleanup, malformed JSON, preservation of upstream icons, large numbers and unrelated metadata.

```ts
it("removes only the chosen slot", () => {
  const icon: CatalogIcon = {
    src: "https://assets.example/logo.svg",
    mimeType: "image/svg+xml",
    sizes: ["any"],
  };
  const primary = setCatalogIcon(
    '{"server":{},"_meta":{"example.test/keep":1}}',
    "default",
    icon,
  );
  const both = setCatalogIcon(primary, "dark", icon);
  expect(readCatalogIcons(setCatalogIcon(both, "dark", null))).toEqual({
    default: icon,
  });
});
```

- [ ] Run `aube run -F admin test src/pages/registry/registryIcons.test.ts`; expect missing module failure. Implement helpers with `jsonc-parser.modify`/`applyEdits`, not JSON.parse/stringify of the whole record.
- [ ] Build Option C: shared previews using `default` on light and `dark ?? default` on dark; "Using primary" fallback label; two compact rows with uploaded filename (local-only after selection), thumbnail, native SVG file input, replace/remove actions. Existing records without filenames can use "Primary SVG"/"Dark-mode SVG"; do not invent persisted filename metadata. Empty previews must not render broken images.
- [ ] Reuse `adminUploadPlatformImage` and the existing issuer-logo public URL construction pattern. Validate extension/type as early UX only; server validation is authoritative. Only preview the server-returned safe asset URL in `img`, never the local SVG bytes/blob or inline markup. On success apply a standard icon object to the latest JSON draft. Serialize upload operations and disable JSON/Save while uploading to prevent stale completion overwriting newer edits; cancel/close must ignore late completions.
- [ ] On error leave the prior slot/draft intact and show the server validation message. Removing an icon clears only that draft slot; Cancel restores the saved record. Save uses existing conflict handling and persists both JSON and icon edits together. Do not change publish/unpublish behavior.
- [ ] Component tests cover primary-only, both logos, empty, dark-only after removal, accepted upload, rejected SVG, disabled/save-in-flight states, late completion on close, JSON edits reflected in previews, and conflict/retry preserving the draft. Assert accessibility labels and keyboard-operable controls.
- [ ] Run `aube run -F admin test src/pages/registry/registryIcons.test.ts src/pages/registry/RegistryIcons.test.tsx src/pages/registry/RegistryEntrySheet.test.tsx` and `aube run -F admin type-check`. Commit `feat: add catalog logo upload controls to Admin`.

### Task 5: Staff MCP parity without bypassing approvals

**Files:** New `server/internal/adminmcp/tool_write_registry_icons.go`, corresponding test; modify `tool_image_upload.go`, `tool_image_upload_test.go`, `tool_registry.go`, `tool_registry_test.go`, `write_tools.go`, `write_guard.go`, `tool_list_schema_test.go`, `tool_directory_test.go`; extend registry mutation helpers for transaction reuse.

**Consumes:** `CatalogIcons`, parse/replace helpers, transactional asset validation from Task 3, existing platform upload tokens from Task 2.
**Produces:** `prepare_set_registry_icons` proposal tool, typed icon metadata on `get_registry_entry`, and updated SVG upload guidance.

```go
type PrepareSetRegistryIconsInput struct {
    EntryID string `json:"entry_id"`
    ExpectedUpdatedAt string `json:"expected_updated_at"`
    Icons mcpregistry.CatalogIcons `json:"icons"`
    RetryKey string `json:"retry_key"`
}
func (s *Service) SetCatalogIconsTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, token string, icons CatalogIcons) (Entry, error)
```

The `Service` above is `mcpregistry.Service`. The tool sets the full two-slot curated set, not arbitrary JSON; `{}` removes curated icons. Missing slots are removed, explicitly described in the schema/approval preview. Upload remains a direct staff write through the existing single-use mechanism; associating an asset requires the existing proposal approval and execution flow.

- [ ] Write tool tests for unverified/nonstaff refusal, exact UUID target, missing revision/retry key, invalid/private asset, identical retries, stale revision at execution, approval required, and unrelated metadata preservation.
- [ ] Run `mise run test:server ./internal/adminmcp -run 'Test.*(RegistryIcons|ImageUpload|RegistryEntry)' -count=1`; new tool assertions fail before registration.
- [ ] Factor the existing locked registry mutation body into a transaction-accepting private helper; preserve `Save` semantics. Implement `SetCatalogIconsTx` by locking/revision-checking, applying `WithCatalogIcons` to the current record, validating both assets, and updating with the same stored-size/publication rules. Never call a separately committing `Save` from proposal execution. Icon changes and proposal execution must commit atomically.
- [ ] Implement proposal prepare/revalidate/execute using `tool_write_registry_entry.go` as the existing framework reference. Approval preview shows exact entry and before/after icon sets; revalidate revision and public platform assets at execution. Register a distinct operation/descriptor/permission using existing staff write conventions; no bypass labels or automatic approvals.
- [ ] Extend `get_registry_entry` with bounded typed curated icons; keep raw unrelated stored metadata out of safe tool responses. Update upload tool copy to include supported SVG subset and return canonical MIME/length. Existing raster uploads used elsewhere remain supported.
- [ ] Platform MCP decision: no new write tool, because this is staff-only curation. Existing catalog discovery/inspection outcomes remain unchanged; no theme-selection behavior or unrelated logo projection change. Native full-envelope preservation is proven in Task 3, while intentionally narrow Platform MCP summaries remain narrow.
- [ ] Run targeted MCP tests plus tool schema/directory tests. Commit `feat: support approved catalog icon updates through Admin MCP`.

### Task 6: Local fixture, end-to-end evidence, and final review

**Files:** `server/internal/demoseed/local.go`, `local_test.go`; create `server/internal/demoseed/fixtures/catalog-logo.svg`; update this plan/spec with test results and justified surface exclusions.

- [ ] Extend the existing local-only global catalog fixture with one tiny fictional SVG using allowed shapes. Store it through the platform image service, and attach `default` plus an optional dark asset using the same validated metadata flow. Hash-dedup makes repeat runs stable; use the fixture's existing global catalog identity. Do not embed local URLs or byte assets into production catalog rows.

```xml
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#8b5cf6" d="M4 20 L12 4 L20 20 Z"/></svg>
```

- [ ] Keep production org-scoped demo phases from modifying global registry rows. Existing `RunLocalFixtures` comments identify the global registry fixture as local-only. Record demo exclusion: no customer-dashboard rendering changes in this slice, and catalog assets are global staff data, not tenant-owned demo data. Add local reseed tests proving stable association and no duplication; use in-memory/local blob storage, not external logo hosts.
- [ ] Run `mise run test:server ./internal/demoseed -run 'TestLocalFixtures' -count=1`. If shared seed phases change despite this plan, activate the demo safety workflow and run `mise run test:server -tags=demoseed_safety ./internal/demoseed/...` before proceeding.
- [ ] Use pitchfork skill for stack operations and gram-playwright-cli for actual Admin browser verification. Upload a safe primary, add dark alternative, replace/remove it, Save/reload, cancel a draft, reject unsafe SVG and confirm the saved logo stays unchanged. Test direct asset navigation: scripts do not execute and requests for external resources are blocked. Record actual response headers. Do not substitute screenshots of the mock for product verification.
- [ ] Verify real catalog list/detail responses expose only `default`/`dark` under envelope metadata; `server.icons` remains unchanged. Repeat current startup seed/merge and verify curated metadata survives. Check transformed asset bytes match the stored hash/length.
- [ ] Run the final focused suite:

```bash
mise run test:server ./internal/assets ./internal/mcpregistry/... ./internal/externalmcp ./internal/adminmcp
aube run -F admin test src/pages/registry
aube run -F admin type-check
git diff --check
mise run lint:brand
```

- [ ] Capture a real UI screenshot and list actual passing commands. Request a security/correctness review of SVG validation, actual HTTP headers, namespace ownership, revision handling and MCP proposal atomicity. No completion claim without evidence. Commit `test: cover catalog icon upload and persistence workflow` with fixture/test changes.

## Plan self-review

- Scope covered: two-slot metadata; unchanged upstream; compact UI; SVG-only curation; authenticated uploads/public reads; security headers; bounded parser; storage reuse; sync/seed preservation; agent parity; tests and fixtures.
- Constructor/configuration changes are confined to providing a canonical asset base URL. No database migration, new CDN, new worker, or customer-dashboard rendering task.
- All cross-task interfaces are declared above. Existing generated clients are regenerated rather than hand-edited.
- No unresolved product questions. Unsupported SVGs are rejected with export guidance; dark-only metadata is tolerated after independent removal rather than silently deleting another slot.

## Execution record — 2026-10-28

Implementation is committed on `walker/catalog-logos`. All six task implementation/automated-review gates passed. Live browser acceptance remains blocked; this is not an end-to-end or deployment sign-off. The checklists above preserve the original execution instructions; this section records actual outcomes.

### Implemented

- Strict, bounded static-SVG policy; canonical platform uploads; CSP and nosniff on both public and Admin image responses.
- Curated `default`/`dark` namespace with canonical public URL and platform-SVG ownership validation; upstream icons unchanged.
- Compact Admin controls, lossless shared JSON draft, protected upload lifecycle, and server-returned public URLs for distinct Admin/public origins.
- Approved staff MCP icon updates with atomic proposal execution; read projection revalidates historical icon URLs/asset ownership and fails closed.
- Local-only fictional catalog fixture, CLI site URL wiring, reseed and persistence/discovery regressions. No production tenant demo mutation or customer-dashboard rendering change.

### Verification evidence

All server test commands used command-scoped `DOCKER_HOST` from the active Docker context; no persistent Docker configuration was changed.

| Check                                                                         | Result                                                               |
| ----------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| Assets, registry and nested packages, external catalog, staff MCP, Admin API  | 2,609 tests passed                                                   |
| Final legacy-projection fix: staff MCP, registry, Admin API (`-p 1 -count=1`) | 1,971 tests passed; independently rerun by controller                |
| Admin registry, issuer editor and API-client frontend suites                  | 179 tests passed                                                     |
| Tagged local-fixture tests                                                    | 2 tests passed                                                       |
| Demo/startup seed CLI tests                                                   | 6 tests passed                                                       |
| Admin type-check and type-aware lint                                          | Passed; zero lint warnings/errors                                    |
| Product naming and diff whitespace                                            | Passed                                                               |
| Whole-branch independent review                                               | Important legacy-projection finding fixed; scoped re-review approved |

The affected Go suites include mounted HTTP upload/create/read/serve checks with distinct origins, CSP/nosniff assertions, and public/raster regression coverage. These are automated HTTP tests, not live deployment verification.

### Remaining acceptance / environment blocker

A local live walkthrough could not start: the Admin service reported an invalid base64 encryption key, and the public server reported a missing JWT signing key. No keys were regenerated or replaced. Dependencies started for the attempt were stopped afterward.

Still required after local setup is repaired: actual Admin upload/save/reopen/reseed walkthrough, browser direct-SVG behavior, actual-product screenshot, and inspection of headers on the deployed/proxied public asset path. The saved Option C screenshot remains a mockup reference only. No push, pull request, merge or deployment was performed.

### Review-driven corrections

- Reject BOM/encoded XML disguised as raster uploads before storage.
- Return canonical public URLs from the Admin upload endpoint rather than deriving them from the Admin browser origin.
- Forward security headers through the Admin serving adapter as well as the public route.
- Verify legacy metadata against trusted origin and live platform assets before exposing it through the restricted staff MCP projection.

### Local browser acceptance follow-up — 2026-10-28

The earlier local startup blocker was repaired with development-only key/IDP/runtime setup, local PostgreSQL and ClickHouse migrations, and local certificates. System certificate trust was not changed: macOS trust installation needs interactive sudo, so the isolated test browser used `ignoreHTTPSErrors`. No production configuration was changed.

Actual Admin browser checks passed against the fictional local catalog entry:

- Primary and dark SVG uploads, explicit Save, close/reopen, and loaded previews.
- Primary upload reused the existing asset because its bytes match the seeded SVG (content deduplication).
- Removing dark in the draft displayed “Using primary”; discarding that draft preserved saved values.
- An SVG containing an `onload` attribute was rejected with “invalid SVG image”.
- Local reseed followed by reload/reopen retained the saved white dark-mode logo and purple primary logo.
- Actual-product screenshot captured and visually inspected at `.playwright-cli/catalog-icons-product.png` (ignored local evidence, not the mockup).
- The local public proxy returned HTTP 200, `image/svg+xml`, restrictive SVG CSP including `sandbox`, and `X-Content-Type-Options: nosniff`.

Browser CLI commands subsequently stalled; direct SVG document navigation was not confirmed. Deployed/proxied production behavior remains untested. Local services were left running for inspection. No application source changes were needed for this follow-up.
