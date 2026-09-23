# Gram-native MCP registry — Stage A implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give staff a validated, editable Gram-owned catalog while every customer and worker registry consumer remains Pulse-backed.

**Architecture:** Add one global Postgres table and one concrete `mcpregistry.Service`, an explicit approved-snapshot importer, and staff-only API/UI operations. Enforce an ordered endpoint-structure freeze in Stage A; legacy Pulse writers cannot participate in native entry locks. Separate the schema-only release from application changes; there is no provider flag, loopback HTTP, or runtime dependency on the standalone repository.

**Tech Stack:** Go, pgx/sqlc, Atlas, Goa-generated admin API/SDK, existing staff-admin OIDC/cookie middleware, React/TanStack Query, existing Sheet/textarea, `santhosh-tekuri/jsonschema/v6`, and workspace-catalogued `@cfworker/json-schema` for editor feedback.

**Spec:** `docs/superpowers/specs/2026-09-21-gram-native-mcp-registry.md` is the planning snapshot of [the canonical Linear design](https://linear.app/speakeasy/document/gram-native-mcp-registry-and-pulse-cutover-design-draft-670077e87781). Read both plan and spec. Parent [GRW-144](https://linear.app/speakeasy/issue/GRW-144); children GRW-151 (store/safety), GRW-129 (import), GRW-156 (API), GRW-154 (editor).

**Historical planning status:** Detailed implementation plan with available source inputs. The approved discovery-preview and Goa/SDK amendments are reconciled to canonical Linear and the owning tickets. The design owns behavior; this plan owns execution steps; tickets own deliverables and acceptance evidence. The historical standalone checkpoint is not an execution prerequisite. Task 0 vendors and validates the existing checked-in catalog/schema pinned below; it is ordinary implementation work, not a request for a missing archive. No application implementation, generator execution, database tests or browser tests were performed during planning.

**Foundation implementation snapshot:** Task 0 contract, shared conformance harnesses, and approved two-record baseline are now checked in; Task 1 schema artifacts are present. These artifacts do not establish completion of later application, database integration, browser flows, or Stage B work. See the contract README for reproducible validation commands.

## Global constraints

- One global, Speakeasy-managed catalog, not a catalog per organization.
- Prepare the three standard read routes through the same service (Task 5a), disabled by default pending acceptance. No standard writes, history store or consumer cutover. Discovery-only behavior is settled in spec section 2a; incremental sync, history storage and generic-client OAuth additions are out of scope.
- Customer registry reads accept a Gram session cookie OR a valid API key under Gram's normal authorization rules. No anonymous read access is introduced. Stage A does not change their backing.
- Staff admins use the existing staff-admin login. Ordinary customer sessions/API keys do not grant staff access or global-catalog mutation rights.
- Explicit Save changes the live entry immediately after server validation. No drafts, autosave, review queue, or separate publish step for ordinary edits.
- Unpublish hides an entry from browsing without destroying its data or breaking retained reference lookup.
- Import the already-approved snapshot, not a new Pulse export. Keep Figma excluded. Task 0 uses only the approved **two-record starter** (Vercel and Linear). The historical source inventory is 62, not starter or production acceptance evidence; full-catalog acceptance remains a Stage B gate.
- `id` and `server.name` are immutable after creation. Entry UUIDs are not legacy registry/provider UUIDs. Do not rewrite installed identities.
- Save and visibility changes require an opaque, lossless stale-editor token; every successful mutation advances it.
- Preserve complete extension JSON, remote ordering, and unknown-versus-empty declarations. URL templates are legitimate data, not verified endpoints.
- No application listeners/work may start without required persistence/config readiness. No fallback to Pulse from an unconfigured native service.
- Migrations ship in their own PR and contain only DDL. Edit `server/database/schema.sql`; generate migrations and `atlas.sum` using Atlas. Never hand-edit generated output.
- No customer-identifying data in committed files, fixtures, file names, PR text or logs published to this public repository.
- Only local databases may be used by agents. Production import/release is an authorized operator procedure, not a planning-agent action.
- No icon uploads in Stage A. Imported CDN URLs remain untouched. Standard UI navigation/close icons are not prohibited.

## Source inputs and verified planning evidence

Inspected Gram commit: `4e1fef388a`. The design references standalone checkpoint `4206342` as historical context for the architecture it replaces, not as a mandatory source artifact. The user clarified that recovering this checkpoint is unnecessary.

Use the existing checked-in inputs from `speakeasy-api/mcp-registry`, pinned to `c5873aaaad0a2988e39f1ed7c237006bbfb05826` rather than a moving branch. A downloaded archive of that revision was inspected: excluding `data/servers/example.json` yields **62 records with 62 unique names**; the record names exclude `com.figma.mcp/mcp`; every record has a string version and nonempty remotes. The largest source record is **3,804,580 bytes**.

Schema source is `packages/registry-api/src/Schema.ts`, SHA-256 `775a4ee2c905874c4b3155452e3fc234d155ef79cb4fb17f40c46f2a9aa70fb5`. It includes custom URI/date filters and optional remotes, so converting it to the canonical Gram JSON Schema still needs conformance tests and enforcement of the agreed remote-record requirements. These are implementation checks, not missing source inputs. No new Pulse export or live verification was performed. Exact record hashes and request-envelope measurements are recorded by Task 0.

## Release slices and scope accounting

| Slice   | Deliverable                                                            | Linear ownership            | Gate                                                                            |
| ------- | ---------------------------------------------------------------------- | --------------------------- | ------------------------------------------------------------------------------- |
| A0      | Vendor pinned catalog and canonical contract                           | GRW-129 / GRW-151           | Task 0 source/conformance checks pass                                           |
| A1      | Additive schema only                                                   | GRW-151                     | Separate migration PR deployed before native application                        |
| A2      | Validated store, Stage A mutation policy, explicit importer, staff API | GRW-151 / GRW-129 / GRW-156 | Real DB/auth tests, local import evidence                                       |
| A2-read | Standard read-only adapter, controlled exposure                        | GRW-156                     | Task 5a discovery/auth/routing tests; explicit limited-capability documentation |
| A3      | Staff editor, seed-preservation checks, Stage A verification           | GRW-154                     | Browser checks and unchanged Pulse consumers                                    |

A2/A3 may be split into reviewable dependent PRs; they do not require a single giant PR. Do not add an independent Pulse source-refactor release.

**Important boundary with GRW-151:** its eventual shared reference-creation locking requirements span A/B. Stage A provides safe administration through a conservative immutable endpoint projection; it does **not** prove legacy writers use the lock. Keep the cross-consumer lock/race work explicitly open for GRW-131 before Stage B unlocks edits. Do not mark the full cross-stage acceptance complete based on Stage A admin-lock tests. Stage A's gate is the enforced restriction, not completion of a prematurely broadened writer migration.

## File map

New:

- `server/design/mcpregistry/`: proposed Goa designs for all three standard read operations, registered in the existing generation pipeline.
- `server/internal/mcpregistry/`: implementations of the generated service interface and real HTTP contract tests; use generated transports, not a parallel handwritten routing contract.
- `server/internal/mcpregistry/{service.go,validation.go,queries.sql,import.go}`: concrete service, strict canonical validation, SQL queries and explicit import.
- `server/internal/mcpregistry/{setup_test.go,service_test.go,validation_test.go,import_test.go}`: local PostgreSQL harness and contract/race/import tests.
- `server/internal/mcpregistry/contract/{record.schema.json,conformance.json}`: one vendored self-contained schema and shared valid/invalid fixture cases, sourced in Task 0.
- `server/internal/mcpregistry/baseline/{manifest.json,records/*.json}`: approved two-record starter input and hashes, after Task 0 validation and public-data check.
- `server/internal/mcpregistry/repo/`: SQLc-generated, not handwritten.
- `server/internal/admin/{registry.go,registry_handler_test.go}`: adapters, staff attribution and real HTTP tests.
- `server/cmd/gram/{registry_import.go,registry_import_test.go}`: explicit import command.
- `client/admin/src/routes/registry.tsx` and `pages/registry/{index.tsx,RegistryEntrySheet.tsx,RegistryEntrySheet.test.tsx}`.
- `client/admin/src/lib/{registryValidation.ts,registryValidation.test.ts}`.

Modify:

- `server/database/schema.sql`, `server/database/sqlc.yaml`.
- Atlas-generated migration/`atlas.sum` in a schema-only PR.
- `server/design/admin/design.go`: operation registration; `server/design/admin/registry.go`: types and registry DSL helper.
- `server/internal/admin/{impl.go,setup_test.go,generated_routes_test.go}`.
- `server/cmd/gram/{admin.go,root.go}`.
- `server/internal/demoseed/safety_test.go` and `seed/demo/{README.md,PAGES.md}`; no global import in tenant SQL.
- `client/admin/src/{components/app-sidebar.tsx,lib/gramAdminClient.ts}` and `client/admin/package.json`; lockfile only through the repo package manager.
- Generated Goa/OpenAPI/SDK and `client/admin/src/routeTree.gen.ts` through generators only.

Leave existing customer routing/backing untouched in `server/internal/externalmcp/impl.go`, worker composition and Platform catalog composition. Task 5a may add a separately controlled adapter mount in server startup and resolve the local-fixture mount in `server/cmd/gram/platform_mcp.go`; it must not rewire existing consumers. No changes to attachment/receipt persistence just to satisfy Stage A.

## Concrete interfaces

Use one service, not a provider abstraction. `service.go` defines the initial store/admin surface below. Task 5a extends this same service with discovery-specific full-record pagination and filters; the HTTP adapter must not own catalog policy or implement a list-summary/GET-per-row loop. `Entry.Data` is the stored JSON document, not a reconstructed typed projection.

```go
type Entry struct {
    ID uuid.UUID
    Data json.RawMessage
    Published bool
    CreatedAt time.Time
    UpdatedAt time.Time
}
type Issue struct { Path string; Message string }
type Invalid struct { Issues []Issue }
func (e *Invalid) Error() string { return "invalid registry record" }
type ListOptions struct {
    Query string
    Published *bool // nil is all, for staff
    Cursor string
    Limit int32 // default 25; max 50
}
type Summary struct {
    ID uuid.UUID
    Name string
    Published bool
    UpdatedAt string
    Issues []Issue
}
type Page struct { Entries []Summary; NextCursor string }
type Service struct { db *pgxpool.Pool; validator *Validator }
func New(db *pgxpool.Pool, validator *Validator) *Service
func (s *Service) Ready(ctx context.Context) error
func (s *Service) List(ctx context.Context, opts ListOptions) (Page, error)
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Entry, error)
func (s *Service) GetByName(ctx context.Context, name string) (Entry, error)
func (s *Service) Create(ctx context.Context, raw json.RawMessage) (Entry, error)
func (s *Service) Save(ctx context.Context, id uuid.UUID, token string, raw json.RawMessage) (Entry, error)
func (s *Service) SetPublished(ctx context.Context, id uuid.UUID, token string, published bool) (Entry, error)
func Token(e Entry) string { return e.UpdatedAt.UTC().Format(time.RFC3339Nano) }
var (
    ErrNotFound = errors.New("registry entry not found")
    ErrConflict = errors.New("registry mutation conflict")
    ErrStageAStructure = errors.New("endpoint changes unavailable while customers use Pulse")
)
type Validator struct { schema *jsonschema.Schema; schemaJSON []byte }
func LoadValidator() (*Validator, error)
func (v *Validator) Validate(raw json.RawMessage) []Issue
func (v *Validator) SchemaJSON() []byte
```

The staff-admin API uses **`data_json: string`** for full record transport, keeping JSONB as the database representation. This prevents JavaScript number rounding from corrupting arbitrary extensions when saving. Validate a parsed copy in the editor; send the original text, never `JSON.stringify(parsedRecord)`. Browser number-related validation is advisory; the server validates losslessly and remains authoritative. Do not reformat fetched JSON through JavaScript parsing either.

The discovery API instead uses standard structured `server`/`_meta` record envelopes, never the admin `data_json` wrapper. Its Goa-generated service adapters call service-level full-record queries applying local visibility, inherited deletion, search, version and cursor filters before pagination. Preserve extensions in the generated transport; dashboard read parsing must not become an admin write round-trip.

All registry timestamps used as tokens are plain Goa strings **without `FormatDateTime`**. Display dates may be formatted separately. No JavaScript `Date` round-trip for the precondition.

RPCs on the existing `admin` service, with `security.AdminAuth`:

| Goa operation               | Route                               | Request                             | Response                                                 |
| --------------------------- | ----------------------------------- | ----------------------------------- | -------------------------------------------------------- |
| `listRegistryEntries`       | GET `/admin/registry.list`          | query?, published?, cursor?, limit? | summaries + next_cursor?                                 |
| `getRegistryEntry`          | GET `/admin/registry.get`           | id                                  | id, data_json, published, created_at, updated_at, issues |
| `getRegistrySchema`         | GET `/admin/registry.schema`        | none                                | schema_json, sha256                                      |
| `createRegistryEntry`       | POST `/admin/registry.create`       | data_json                           | entry                                                    |
| `saveRegistryEntry`         | POST `/admin/registry.save`         | id, updated_at, data_json           | entry                                                    |
| `setRegistryEntryPublished` | POST `/admin/registry.setPublished` | id, updated_at, published           | entry                                                    |

400 malformed request/cursor/token; 401/403 existing staff auth; 404 missing row; 409 stale/name/Stage-A structural conflict; 422 schema or immutable-name violation. Reuse `oops` and shared HTTP mappings. Prefix safe messages with JSON Pointer paths; do not echo record values, secrets or tenant reference data. Staff read results include structured `issues` arrays; no global redesign of error envelopes is necessary.

## Task 0 — Vendor and validate the existing catalog and schema

**Files:** new `mcpregistry/contract/`, `baseline/`; public provenance notes in `baseline/manifest.json`.

**Consumes:** existing `speakeasy-api/mcp-registry` catalog/schema at `c5873aaaad0a2988e39f1ed7c237006bbfb05826`; no historical checkpoint or fresh Pulse export.

**Produces:** self-contained canonical JSON Schema with explicit dialect, shared fixtures, two exact starter record files, and a manifest with full revision and per-file SHA-256 hashes.

- [x] **Acquire the pinned existing source**, without invoking any Pulse acquisition script:

```sh
export APPROVED_REGISTRY_SOURCE="$(mktemp -d /tmp/gram-registry-input.XXXXXX)"
gh api repos/speakeasy-api/mcp-registry/tarball/c5873aaaad0a2988e39f1ed7c237006bbfb05826 \
  | tar -xz -C "$APPROVED_REGISTRY_SOURCE" --strip-components=1
```

The source directory is temporary and is not committed. Vendor only the reviewed catalog/schema inputs into Gram; retain no runtime dependency on the standalone repository. Pin the full revision in the manifest, and do not silently advance it with future remote-main changes.

- [x] **Pin and validate the input set.** Select only `data/servers/app.linear__linear.json` and `data/servers/com.vercel__vercel-mcp.json`; do not copy the other 60 records. Check record names rather than filenames for Figma, and fail unless count/uniqueness/payload shape match approval. Use this exact inventory check before copying:

```python
import hashlib, json, os
from pathlib import Path
root = Path(os.environ['APPROVED_REGISTRY_SOURCE']) / 'data/servers'
files = [root / name for name in ['app.linear__linear.json', 'com.vercel__vercel-mcp.json']]
assert len(files) == 2
names = []
for p in files:
    raw = p.read_bytes()
    record = json.loads(raw)
    name = record['server']['name']
    assert name != 'com.figma.mcp/mcp'
    assert isinstance(record['server']['version'], str)
    assert record['server'].get('remotes')
    names.append(name)
    print(p.name, len(raw), hashlib.sha256(raw).hexdigest())
assert len(set(names)) == 2
```

- [x] **Port the canonical schema without introducing dual handwritten models.** Vendor/export the approved full-record schema as `contract/record.schema.json`; bundle referenced schemas offline, preserve open extension metadata, remote requirements and supported URL templates. Confirm the explicit dialect and assert format semantics in both validators using existing source tests and shared negative fixtures ported with the contract. Unsupported constraints fail schema initialization rather than become no-op matches. Translate the source custom filters into faithfully enforced schema/format semantics and add negative cases; do not silently drop a filter during export. Resolve any demonstrated conformance discrepancy during this task rather than assuming a historical archive contains the answer.
- [x] **Record byte limits and provenance.** Write manifest fields `source_revision`, `schema_sha256`, `records:[{file,name,sha256}]`, `count`, `max_record_bytes` and `planned_max_mutation_envelope_bytes`, measured using the documented admin JSON wrapper. The generated API does not exist until Task 5: its actual SDK serialization/envelope measurement is a Task 5 release check, not a Task 0 dependency. Proposed service limits are 8 MiB raw record and 16 MiB registry mutation envelope; reject the proposal if the approved largest fixture cannot round-trip. Keep record bodies out of logs/review output.
- [x] **Check public suitability**, including file names and URLs; never copy credentials/customer identifiers. Keep confidential operational evidence in Linear, not the baseline files.
- [x] Commit the approved data/schema slice only after provenance and conformance pass. No new Pulse acquisition, runtime export dependency or boot-time import.

**Expected check:** success produces exactly two hash-pinned starter records and one validated schema contract. Task 0 implementation and independent review passed for the two-record starter: Go contract tests, 34 browser tests, pinned-source export/fixture/hash parity and public-content review. Minimal contract compilation was brought forward from Task 2 to verify parity; later validation code must reuse it. Database import remains Task 4, and actual generated-SDK envelope measurement remains Task 5.

## Task 1 — Add the catalog table in a schema-only PR

**Files:** `server/database/schema.sql`; generated migration and `server/migrations/atlas.sum`.

**Consumes:** storage contract; no imported data. **Produces:** additive table available before A2 application deployment.

**Publication evidence (2026-09-22):** Schema-only PR [#6644](https://github.com/speakeasy-api/gram/pull/6644), branch `walker/mcp-registry-schema`, commit `aa8d7ac791`, targets `main`. Independent review approved the DDL before publication. Its diff includes `schema.sql`, the Atlas-generated migration and `atlas.sum`, plus the inert SQLc database model mechanically generated for CI consistency (`cfa30f79e1`); no application data or business logic. Latest-main migration ordering, structural migration lint and `atlas migrate validate` pass. Full Atlas lint could not be rerun during publication because the local database on port 5439 was unavailable; no production database was accessed. Deployment and local apply/uniqueness evidence are not established by this publication step.

**Application stack:** `walker/mcp-registry-foundation` targets `walker/mcp-registry-schema` with Task 0 and planning; the approved two-record source hashes remain unchanged. Integrated Go contract checks and all 34 JavaScript contract tests pass. Continue Task 2 on a new branch based on this foundation; do not duplicate DDL into its diff. The schema PR must be deployed before releasing the future service. All future API work uses Goa and generated SDKs; the registry stays discovery-only with one mutable record and no synchronization or version history.

- [x] Add this SDL next to registry tables:

```sql
CREATE TABLE IF NOT EXISTS mcp_registry_entries (
  id uuid PRIMARY KEY DEFAULT generate_uuidv7(),
  data jsonb NOT NULL,
  published boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX mcp_registry_entries_name_key
  ON mcp_registry_entries ((data #>> '{server,name}'));
```

Service validation requires the name; this migration does not add enumeration/value checks, legacy foreign keys, an organization column or a second name column.

- [ ] Generate and lint locally:

```sh
mise run db:diff mcp_registry_entries
mise run lint:migrations
```

- [ ] Apply only to the local database using `mise run db:migrate`; prove uniqueness includes unpublished rows in a rollback-only local SQL check:

```sql
BEGIN;
INSERT INTO mcp_registry_entries(data,published)
VALUES ('{"server":{"name":"example.test/demo"}}', false);
DO $$ BEGIN
  BEGIN
    INSERT INTO mcp_registry_entries(data)
    VALUES ('{"server":{"name":"example.test/demo"}}');
    RAISE EXCEPTION 'unique index did not reject duplicate';
  EXCEPTION WHEN unique_violation THEN NULL;
  END;
END $$;
ROLLBACK;
```

- [ ] Commit only SDL/generated DDL/hash changes. Use the repository migration PR workflow; no application, import DML, seed data or unrelated changes in this PR. Confirm deployment before an authorized operator releases A2. Preserve additive data under application rollback.

## Task 2 — Validate and read complete records through one service

**Files:** `mcpregistry/{validation.go,service.go,queries.sql,setup_test.go,validation_test.go,service_test.go}`, SQLc stanza; contract files from Task 0.

**Consumes:** table and approved canonical schema. **Produces:** constructor/validator/read interfaces above and generated `repo.Queries`.

- [ ] Write pure conformance tests first. Fixture file shape is `[{"name":string,"record":object,"valid":boolean}]`; the same file is consumed by Go and TypeScript. Include valid templates, six remotes, extension objects/large integers, invalid names/version/remotes, malformed schema patterns and URI/date boundaries from approved fixtures.

```go
func TestContractConformance(t *testing.T) {
    v, err := LoadValidator()
    require.NoError(t, err)
    raw, err := os.ReadFile("contract/conformance.json")
    require.NoError(t, err)
    var cases []struct {
        Name string `json:"name"`
        Record json.RawMessage `json:"record"`
        Valid bool `json:"valid"`
    }
    require.NoError(t, json.Unmarshal(raw, &cases))
    for _, tc := range cases {
        t.Run(tc.Name, func(t *testing.T) {
            require.Equal(t, tc.Valid, len(v.Validate(tc.Record)) == 0)
        })
    }
}
```

Run `mise run test:server ./internal/mcpregistry/ -run TestContractConformance`; first run fails until the validator exists.

- [ ] Implement strict compilation directly through the library, **not** `internal/jsonschema`'s permissive regex helper:

```go
// Embed contract/record.schema.json with go:embed in validation.go.
compiler := jsonschema.NewCompiler()
compiler.AssertFormat()
schemaValue, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
if err != nil { return nil, fmt.Errorf("decode registry schema: %w", err) }
if err := compiler.AddResource("urn:gram:mcp-registry:record", schemaValue); err != nil {
    return nil, fmt.Errorf("register registry schema: %w", err)
}
compiled, err := compiler.Compile("urn:gram:mcp-registry:record")
if err != nil { return nil, fmt.Errorf("compile registry schema: %w", err) }
```

Reject remote `$ref` dependencies at initialization; all schema resources must be bundled. `Validate` bounds raw bytes, uses `jsonschema.UnmarshalJSON` to preserve `json.Number`, requires exactly one JSON value, validates the complete object, and translates errors to bounded JSON Pointer messages without rejected values. Include the semantic requirement for a nonempty string name before writes; preserve raw payload in storage.

- [ ] Add SQLc stanza matching existing admin configuration, with queries in `../internal/mcpregistry/queries.sql` and generated output `../internal/mcpregistry/repo`. Add named queries `GetEntry`, `GetEntryByName`, `ListEntries`, `LockEntry`, `CreateEntry`, `UpdateEntry`, `SetEntryPublished`, `InsertImportedEntry`.

```sql
-- name: GetEntry :one
SELECT * FROM mcp_registry_entries WHERE id = @id;

-- name: GetEntryByName :one
SELECT * FROM mcp_registry_entries WHERE data #>> '{server,name}' = @name;

-- name: LockEntry :one
SELECT * FROM mcp_registry_entries WHERE id = @id FOR UPDATE;
```

`ListEntries` searches by case-insensitive substring of the name, uses immutable name ordering with explicit `COLLATE "C"` plus UUID, and `limit+1`. Cursor carries last name/ID plus the search and visibility filter, encoded as base64url JSON. Reject malformed/oversized cursors and filter mismatches; default limit 25, max 50. Staff list returns summaries/diagnostics, not all large JSON bodies. Get/retained GetByName never filter publication. A corrupt row can still be returned as raw JSON with diagnostics.

- [ ] Build local harness by copying `admin/setup_test.go`'s `testenv.Launch`/`CloneTestDatabase` pattern, PostgreSQL only. Define `newTestService(t) (context.Context,*Service,*pgxpool.Pool)` returning `t.Context()`, a fresh cloned DB and `New(db,validator)`; call `LoadValidator` and require success. Add tests named `TestListPagination`, `TestRetainedUnpublishedGet`, `TestInvalidStoredRecordReadable`, `TestLargeNumberRoundTrip`, `TestReadyMissingTable`.
- [ ] `Ready` compiles the schema and checks the table with a bounded query before the admin listener starts; an empty table is valid **in Stage A before explicit import**, not proof of readiness for Stage B cutover.
- [ ] Run `mise run gen:sqlc-server`, then `mise run test:server ./internal/mcpregistry/`. Commit service/read/contract tests with generated SQLc output; do not wire customer readers.

## Task 3 — Make staff mutations atomic and safe during the Pulse-backed period

**Files:** `mcpregistry/{service.go,validation.go,queries.sql,service_test.go}`.

**Consumes:** concrete service/read methods. **Produces:** Create, Save, SetPublished; immutable endpoint projection; conflict semantics.

- [ ] Write the stale-token and structural-policy tests before mutation code:

```go
const basicRecord = `{"server":{"name":"example.test/demo","description":"Demo","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]}}`

func TestStaleSaveAndUnpublish(t *testing.T) {
    ctx, svc, _ := newTestService(t)
    entry, err := svc.Create(ctx, json.RawMessage(basicRecord))
    require.NoError(t, err)
    token := Token(entry)
    updated := strings.Replace(basicRecord, `"Demo"`, `"Updated"`, 1)
    _, err = svc.Save(ctx, entry.ID, token, json.RawMessage(updated))
    require.NoError(t, err)
    _, err = svc.SetPublished(ctx, entry.ID, token, false)
    require.ErrorIs(t, err, ErrConflict)
    got, err := svc.Get(ctx, entry.ID)
    require.NoError(t, err)
    require.True(t, got.Published)
    require.NotEqual(t, token, Token(got))
}

func TestStageARejectsEndpointChangeWithoutReferences(t *testing.T) {
    ctx, svc, _ := newTestService(t)
    entry, err := svc.Create(ctx, json.RawMessage(basicRecord))
    require.NoError(t, err)
    changed := strings.Replace(basicRecord, "https://example.test/mcp", "https://example.test/new", 1)
    _, err = svc.Save(ctx, entry.ID, Token(entry), json.RawMessage(changed))
    require.ErrorIs(t, err, ErrStageAStructure)
}
```

Run `mise run test:server ./internal/mcpregistry/ -run 'TestStale|TestStageA'` and see missing mutation behavior fail.

- [ ] Implement `Create` with strict validation, unique-name conflict translation and published=true. The caller cannot supply an entry ID. Implement Save/visibility as: begin transaction → `LockEntry` → compare parsed lossless token → validate policy → update → commit. Do not open network calls inside that transaction.
- [ ] Preserve publication on Save. Validate before publishing; unpublishing does not require record-schema validity. Use stored timestamp values and advance even for accepted no-op mutations:

```sql
-- name: UpdateEntry :one
UPDATE mcp_registry_entries
SET data = @data,
    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
WHERE id = @id
RETURNING *;

-- name: SetEntryPublished :one
UPDATE mcp_registry_entries
SET published = @published,
    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
WHERE id = @id
RETURNING *;
```

These queries are only called on the transaction after the row lock/precondition check, never directly from handlers.

- [ ] Freeze **ordered `(type,url,variables)` projections** of existing `server.remotes` for Stage A; compare lossless JSON values for variables. Reject insertion/removal/reordering/transport/URL/template-input changes regardless of current reference count. This is conservative by design. Metadata and header/evidence edits remain subject to schema and immutable-name checks; publication still works. No flag relaxes the policy. Error paths identify the forbidden structural field without exposing installation data.
- [ ] Because structure cannot shift in Stage A, populated positional enrichment cannot silently reattach through a structural save. Add a fixture with distinct evidence at indices 0/1 and assert reorder fails; explicit evidence removal alone remains possible. Do not automatically clear evidence or reinterpret absence as empty tools.
- [ ] Add barrier-based concurrent tests: two Save calls and Save/Unpublish using the same token yield exactly one success and one conflict. Also test duplicate-name concurrent Create, immutability, invalid writes leaving data/token unchanged, Save on unpublished rows and invalid-row Unpublish/Republish.
- [ ] Run focused tests followed by `mise run test:server ./internal/mcpregistry/`. Commit. Do not claim tests prove installation-writer participation; Stage A succeeds precisely because incompatible endpoint edits are unavailable.

## Task 4 — Import the approved baseline explicitly and preserve it across reseeds

**Files:** `mcpregistry/{import.go,import_test.go}`, approved baseline from Task 0; `server/cmd/gram/{registry_import.go,registry_import_test.go,root.go}`; `demoseed/safety_test.go`, `seed/demo/{README.md,PAGES.md}`.

**Consumes:** manifest, store and canonical validator. **Produces:** explicit local/operator import with reproducible outcomes and no boot-time/reseed overwrite.

```go
type ImportSummary struct { Inserted, Unchanged int; Conflicts []string }
func (s *Service) Import(ctx context.Context, source fs.FS) (ImportSummary, error)
```

- [ ] Write tests `TestImportApprovedCountAndHashes`, `TestImportRerunKeepsIDs`, `TestImportRejectsHashMismatch`, `TestImportPreservesStaffEdit`, `TestImportConcurrentCreate` before implementing the importer. Use `testing/fstest.MapFS` for synthetic records/manifest; use the approved baseline for the two-record starter conformance test. Tests hash exact source bytes, not reserialized JSON.
- [ ] Validate the **entire** manifest and every record before beginning a transaction. Require approved count, no duplicate names, excluded fixture/Figma, schema conformance and exact hashes. Report file/name/hash/count summaries, never payload contents.
- [ ] Within one transaction, use the `InsertImportedEntry` query below per record, then read the existing row by name when no insert occurred. Compare database JSONB equality, not whitespace. Equal rows are unchanged; differing rows are conflicts, never overwritten. If any conflict exists, roll back new inserts and return the conflict summary/error. This makes rerun-after-edit non-destructive and visible, not a silent reset. Preserve visibility and UUIDs.

```sql
-- name: InsertImportedEntry :one
INSERT INTO mcp_registry_entries(data,published)
VALUES (@data,true)
ON CONFLICT ((data #>> '{server,name}')) DO NOTHING
RETURNING *;
```

- [ ] Add `newRegistryImportCommand()` in `server/cmd/gram/registry_import.go` and register it in `root.go`, following `newDemoSeedCommand` only for CLI/dependency conventions. Command is `gram registry-import --source <approved-local-directory> --dry-run`; require explicit `--apply` for writes and make dry-run/apply mutually exclusive. No network acquisition, default remote DB, startup invocation or provider flag. Use existing DB configuration/dependency loading; dry-run validates source and reports existing-row conflicts without writes.
- [ ] CLI tests prove invalid source and missing explicit mode exit nonzero, dry-run leaves DB unchanged, and apply/rerun outputs counts without record contents. `registry_import_test.go` tests parsing/command behavior against local test dependencies, not production.
- [ ] Keep the global import **out of** `demoseed/postgres.sql` and the daily tenant seed. Add a seed-safety regression that preloads two synthetic global registry entries (one staff-edited/unpublished), runs both tenant seed cycles, and compares ID/data/publication/timestamps byte-for-byte. No table exemption in safety fingerprinting.
- [ ] Document explicit local import as a separate setup step, preserve existing Pulse-backed demo installations, and explain why staff-only administration is not exposed to the demo customer. No demo-prefixed duplicate catalog identities.
- [ ] Verify:

```sh
mise run gen:sqlc-server
mise run test:server ./internal/mcpregistry/ ./cmd/gram/ -run 'TestImport|TestRegistryImport'
mise run test:server -tags=demoseed_safety ./internal/demoseed/ -run 'TestDemoSeedSafety|TestRegistry'
```

- [ ] Commit importer/fixtures/docs/tests. During authorized local execution, build via `mise run build:server`, run the resulting binary's `registry-import --help`, then explicit dry-run/apply against local configuration. Operator release steps use approved infrastructure processes; this plan does not authorize agents to access remote databases.

## Task 5 — Expose secure staff routes and generate lossless clients

**Files:** `server/design/admin/{design.go,registry.go}`, `internal/admin/{registry.go,impl.go,setup_test.go,registry_handler_test.go,generated_routes_test.go}`, `cmd/gram/admin.go`; generated Goa/OpenAPI/admin SDK.

**Consumes:** service interfaces and errors. **Produces:** six RPCs in the table above, available to the existing admin SDK only.

- [ ] Add HTTP tests first using `newTestAdminService`. Mount generated handlers with `AdminCORS` and `AdminOriginCheck` as `cmd/gram/admin.go` does; calling `Attach` alone does not exercise origin protections. Cases: valid staff cookie with no customer credentials, expired/invalid cookie, customer cookie/API key, no Origin on unsafe method, foreign Origin, malformed JSON before auth, stale token and over-limit body.
- [ ] Add `registryDesign()` helper called inside the existing admin service DSL. Define `AdminRegistryIssue`, `AdminRegistryEntry`, `AdminRegistrySummary`, `AdminRegistryPage`, and `AdminRegistrySchema`. Use raw JSON strings and plain timestamp strings:

```go
Attribute("data_json", String, "Complete registry record JSON; preserved without client coercion")
Attribute("updated_at", String, "Opaque lossless write precondition; echo unchanged")
Attribute("published", Boolean)
// No FormatDateTime on updated_at; no project slug or customer API-key payload.
```

Annotate operation IDs consistently so generated functions are `adminListRegistryEntries`, `adminGetRegistryEntry`, `adminGetRegistrySchema`, `adminCreateRegistryEntry`, `adminSaveRegistryEntry`, `adminSetRegistryEntryPublished`. Use existing admin tag/overlay conventions; do not add a second SDK/service framework.

- [ ] Inject `*mcpregistry.Service` into the admin Service and local test constructor. Compile the canonical validator, construct service, and call `Ready` before starting the admin listener. Do not change normal server/worker registry wiring.
- [ ] Wrap all new reads with `preauthorizeAdmin`. Refactor `strictAdminJSON` to delegate to `strictAdminJSONLimit(next, body, maxBytes)` with the existing 1 MiB default unchanged for other routes; registry Create/Save use the measured 16 MiB proposed bound from Task 0. Bound service raw JSON at 8 MiB. SetPublished remains small. Auth happens before body reads; reject trailing values and unknown envelope fields, while allowing extensions **inside** `data_json`.
- [ ] Adapters translate `Invalid` to 422 with safe path messages, not raw rejected values; conflict/not-found/bad-token errors map as specified. Use `adminActor` and structured success logs for global mutations with action, actor principal, entry ID/name and resulting token only. Existing org audit rows require an organization ID: **do not fabricate one or add a new audit framework**. Log successful mutation after commit and failures separately, never claiming audit/DB atomicity that does not exist.
- [ ] Generate:

```sh
mise run gen:goa-server
mise run gen:sdk
mise run test:sdk-overlays
mise run test:gen-sdk
mise run test:server ./internal/admin/ ./internal/mcpregistry/
```

Verify exported operation names against annotations and SDK output. Measure the actual largest generated-SDK mutation envelope against the vendored baseline and compare it with the Task 0 planned-envelope measurement and 16 MiB limit; do not treat a planned wrapper measurement as generated-API evidence. Add an SDK round-trip test that deserializes then serializes `updated_at="2026-09-21T12:00:00.123456Z"` and leaves it a byte-identical string. No hand-edited generated files.

- [ ] Commit API, adapters and generated outputs after the real HTTP suite passes.

## Task 5a — Prepare the standard read-only registry adapter

**Ownership:** GRW-156. **Depends on:** Tasks 0/2 and existing customer-auth integration, not the editor. **Contract:** spec section 2a, pinned upstream OpenAPI commit `bf4e88cbe8d1a635c06144ccea1d24cb52fa6186`.

**Files:** proposed `server/design/mcpregistry/` Goa service design, `server/internal/mcpregistry/` service adapters and HTTP tests, existing design registration and server startup wiring, `server/cmd/gram/platform_mcp.go` fixture mount where needed; generated Goa transports, OpenAPI and dashboard-consumable SDK. Confirm package/registration conventions before implementation; do not hand-edit generated output.

- [ ] Define all three registry read routes in Goa, not a parallel handwritten-only HTTP API. Model query parameters, standard envelopes/errors and existing customer security requirements; annotate operation IDs/tags using existing SDK conventions. Keep staff-admin operations separate.
- [ ] Run `mise run gen:goa-server`, `mise run gen:sdk`, `mise run test:sdk-overlays` and `mise run test:gen-sdk`. Verify dashboard-consumable generated operations exist; add SDK-to-mounted-transport tests for encoded names/versions, pagination, auth and complete extension-metadata preservation. Use the generated SDK rather than a bespoke dashboard fetch client when consumer cutover occurs; generation alone does not rewire Stage A consumers.

- [ ] Implement the settled discovery-only contract in spec section 2a. Keep Task 1's single-record schema unchanged: no retained history, removal markers or speculative synchronization columns. Document the unsupported incremental-sync capability.
- [ ] Add failing contract tests for `GET /v0.1/servers`, `GET /v0.1/servers/{serverName}/versions`, and `GET /v0.1/servers/{serverName}/versions/{version}`. Standard list envelope, page count, cursor/end-of-list, record envelope and 404 errors must match the pinned contract. Do not add standard write endpoints or the nonstandard name-only route.
- [ ] Implement name-substring search, bounded immutable-name keyset pagination, exact/`latest` version filtering and query validation. Test `include_deleted` on all three routes against spec section 2a, including official deletion versus local unpublished visibility. Any supplied `updated_since` (valid, empty or malformed) returns typed HTTP 400 identifying the unsupported parameter. Test concurrent membership changes and document live traversal rather than snapshot/mirroring guarantees.
- [ ] Test singleton version lists, `latest` resolving the current row, old-version 404 after replacement, and no historical/immutable-version promise. Test encoded slashes and reserved characters through the actual HTTP router. Preserve raw extension data without JavaScript round-tripping or fabricated upstream status/latest/timestamps.
- [ ] Keep unpublished records unavailable through normal discovery/version lookups; retain saved-reference lookup separately. Test official-deleted versus locally unpublished records, exact lookups, republish and all enabled deletion filters against the discovery visibility policy in spec section 2a.
- [ ] Reuse normal authorized customer cookie/API-key checks, not staff auth. Exercise anonymous, invalid/insufficient credentials and valid cookie/key requests. Document credential transport and representative client support; do not claim generic OAuth interoperability without verifying its discovery/challenge/token path. OAuth additions require separate approval.
- [ ] Add explicit adapter exposure control, disabled by default; this is not a provider selector. Test mount-disabled and mount-enabled behavior, persistence readiness and mutual handling of the existing local-fixture `/v0.1/servers` route. No duplicate routes, Pulse fallback, fixture regression or existing consumer rewiring.
- [ ] Run `mise run test:server ./internal/mcpregistry/` plus the targeted startup/fixture and generated-transport suites selected from touched packages. Attach route-level and representative-client evidence. Release only the documented discovery preview; do not claim full read-contract, incremental-sync or generic OAuth interoperability.

## Task 6 — Build the staff catalog list and explicit-save JSON sheet

**Files:** route/page/sheet/validation paths in file map; `gramAdminClient.ts`, sidebar, package.json, tests and generated route tree.

**Consumes:** generated RPCs and schema endpoint. **Produces:** staff workflow under `/registry` with shared canonical validation and no customer routing changes.

```ts
type RegistryEntrySheetProps = {
  id: string | null; // null creates, UUID edits; never use a mutable display name as key
  open: boolean;
  onOpenChange: (open: boolean) => void;
};
type ValidationIssue = { path: string; message: string };
type Draft = {
  id: string | null;
  dataJSON: string;
  token: string | null;
  published: boolean;
};
// Validation examines a parsed copy; the mutation sends draft.dataJSON verbatim.
```

- [ ] Add workspace `@cfworker/json-schema` dependency to admin through `aube`; reuse the catalogued version. Confirm its dialect/format behavior against Task 0 fixtures; do not introduce a Zod reimplementation of the record schema. Retrieve schema bytes through authenticated `getRegistrySchema`, parse/compile once per schema hash, and disable saving if schema initialization fails visibly.
- [ ] Write `registryValidation.test.ts` using the shared conformance file via a filesystem read in Vitest. Add a transport test for a large integer:

```ts
it("keeps the raw record and opaque token unchanged", () => {
  const draft: Draft = {
    id: "00000000-0000-4000-8000-000000000001",
    dataJSON:
      '{"server":{"name":"example.test/demo"},"_meta":{"n":9007199254740993}}',
    token: "2026-09-21T12:00:00.123456Z",
    published: false,
  };
  const envelope = JSON.parse(
    JSON.stringify({ data_json: draft.dataJSON, updated_at: draft.token }),
  );
  expect(envelope.data_json).toBe(draft.dataJSON);
  expect(envelope.updated_at).toBe(draft.token);
});
```

This fixture tests transport, not record validity. Never replace raw text with a schema validator's transformed value.

- [ ] In `gramAdminClient.ts`, use generated builders with existing same-origin cookie clients and generated cache keys. Export `registryEntriesQuery(params)`, `registryEntryQuery(id)`, `registrySchemaQuery()`, `useCreateRegistryEntryMutation`, `useSaveRegistryEntryMutation`, `useSetRegistryEntryPublishedMutation`; signatures use generated payload/result types. Reads follow existing 401 redirects; mutations preserve drafts on auth/network errors.
- [ ] Add `/registry` route with `createFileRoute`, `crumb: "Registry"`, search/publication filters and paged summaries. Reuse sidebar styling and a normal existing navigation icon; no unrelated sidebar refactor. Provide New/Edit and explicit Unpublish/Republish actions.
- [ ] Use `SheetContent` and a labeled textarea. On opening, load raw `data_json` and exact token once into Draft. Never replace an open dirty draft or its base token on background refetch. Show a Stage A notice: **“Edits affect the Gram catalog. Customer catalog reads still use Pulse.”** Show endpoint-change restrictions.
- [ ] Render syntax/schema errors with paths using `aria-describedby`/`aria-invalid`; retain input on 422/409/network errors. Schema validation is feedback, not the source of truth. Disable Save during load/mutation; explicit Reload after a conflict discards only after confirmation, never auto-retries using a newer token.
- [ ] Visibility actions use their own current token and do not silently save or discard a dirty text draft. Require save/cancel before toggling while dirty. Save on an unpublished entry keeps it unpublished; Create publishes. Cancel/dismiss confirms discarding dirty text and returns focus correctly.
- [ ] On success, use returned entry/token to update detail cache and invalidate all list-filter variants. Delayed responses cannot overwrite the draft. Do not retain a second editable icon/field state.
- [ ] Component tests via `renderWithApp` cover explicit Save only, Cancel/no mutation, validation paths, unpublished Save, dirty visibility controls, stale conflicts, refetch while dirty, largest approved text, and generated-token round-trip. Start each focused test red before implementing its behavior.
- [ ] Verify and commit:

```sh
aube run -F admin test src/lib/registryValidation.test.ts
aube run -F admin test src/pages/registry/RegistryEntrySheet.test.tsx
aube run -F admin build
aube run -F admin type-check
```

Build generates `routeTree.gen.ts` before type-check; do not hand-edit the route tree. Use the relevant React and browser skills during execution. A production build is not a browser-behavior proof.

## Task 7 — Prove Stage A without accidentally performing the cutover

**Files:** existing tests plus `docs/superpowers/plans/2026-09-21-gram-native-registry-stage-a.md` execution checklist/evidence links; `seed/demo/PAGES.md`.

**Consumes:** deployed local Stage A components and approved import. **Produces:** local/browser evidence plus a bounded operator release checklist. No production operations by the planning agent.

- [ ] Verify Task 5a exposure defaults and contract/auth/routing evidence; record whether the discovery preview remains disabled or passes its controlled-release checks.
- [ ] Run the targeted service/admin/CLI suites and seed-safety checks, preserving failures rather than hiding them in a full-build log. Run admin component tests, build and type-check.
- [ ] Load `pitchfork`, `gram-playwright-cli`, and any admin-specific browser guidance before service/browser execution. Start only the local services needed; do not assume dashboard auth automation signs into the separate staff UI. Use the existing local staff OIDC emulator, real cookie/origin path and explicit import command.
- [ ] Browser proof: staff list/search/create/edit, schema errors, stale second tab, unpublish/republish, invalid historical row repair, large record, and Stage A notice. Check network routes are staff RPCs and old customer browse/install still reads Pulse using local mocked/fixture transport where needed; do not call production merely to prove non-regression.
- [ ] Confirm diff contains no Stage B customer/worker rewiring, provider flag, schema cleanup, receipt-identity migration, icon upload implementation or unconditional seed import.
- [ ] Release checklist for an authorized operator: schema-only migration deployed → application persistence/contract readiness → explicit approved import dry-run/apply with hashes/counts → staff API/UI release → representative staff and unchanged customer/worker checks. Abort on conflicts or missing persistence. Reverting application code leaves table/data intact.
- [ ] Keep endpoint-structure freeze in place after Stage A fleet convergence. Record Stage B prerequisites: attachment upsert/clone paths in `deployments/crud.go`; Platform pre-receipt/registration/completion paths; classifiable pending work; revalidation under a shared entry lock; all-retained-reference old/new selection comparison; bounded fleet transition. These are not optional because Stage A tests pass.
- [ ] Report evidence honestly and link it to GRW-144/children. Obtain review via `requesting-code-review`, run `verification-before-completion` before commits/PR completion claims, and use `pull-request` / `pr-demo-gif` when preparing a user-visible PR. Do not bypass required reviews or checks.

## Historical planning self-review and acceptance map

At the planning snapshot (not current implementation status), planning checks: all referenced `mise run` task names were found in this worktree's task inventory; code fences and task structure were checked; only this plan and its spec snapshot are new files. These checks do not execute the proposed code, generators, database tests or browser flows. Pinned source availability and basic inventory checks passed; schema conformance and application behavior remain to be tested during implementation.

| Spec requirement                                                                 | Plan location                                                        |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| Global JSONB, stable identity, unique unpublished names                          | Tasks 1–3                                                            |
| Approved two-record starter, hashes, no fresh export, idempotent import          | Tasks 0/4                                                            |
| Canonical schema, complete metadata, URL templates, strict server validation     | Tasks 0/2/5/6                                                        |
| Staff-only auth, origin checks, safe errors and attribution                      | Task 5                                                               |
| Immediate Save, explicit visibility, repairability                               | Tasks 3/5/6                                                          |
| Lossless token, same-second/racing writes                                        | Tasks 3/5/6                                                          |
| Stage A protects endpoint selection without pretending Pulse writers lock        | Task 3; Stage B gate in Task 7                                       |
| Shared reference-creation lock and pending-work protection                       | Explicit Stage B carry-forward; not falsely claimed implemented in A |
| Unpublished retained reads and coherent per-record data                          | Tasks 2/3; consumer wiring remains Stage B                           |
| Fresh staff reads, cache invalidation, no stale editor replacement               | Tasks 2/6                                                            |
| Global data survives tenant reseeds and staff edits                              | Task 4                                                               |
| Customer/worker Pulse behavior unchanged                                         | Tasks 5/5a/7                                                         |
| Three standard read routes; current-version retention and encoded paths          | Task 5a; spec section 2a                                             |
| Discovery-only limits, explicit sync rejection, no history/sync schema additions | Task 5a; spec section 2a                                             |
| Customer auth, controlled adapter exposure and fixture route safety              | Tasks 5a/7                                                           |
| Separate schema PR and explicit operator deployment                              | Tasks 1/7                                                            |
| Icon Completeness excluded without breaking existing URLs                        | Global constraints, Tasks 0/4/6                                      |

### Remaining execution decisions

No missing-checkpoint or source-recovery question remains. The pinned existing catalog and schema are available. Task 0's full conformance checks and payload-envelope measurements are normal implementation acceptance work.

The conservative Stage A endpoint projection freeze implements the already-agreed mixed-release restriction; it does not implement or declare complete installation-writer lock participation before Stage B. Keep that cross-stage work explicit as described above. No new product decision is required to recover the superseded standalone architecture.

Read-adapter scope is settled by spec section 2a: discovery-only, with no incremental synchronization, retained history or new OAuth scope. Before exposure, verify the supported Gram credential flow and generated SDK against the mounted routes. Canonical Linear and relevant tickets are reconciled; implementation acceptance remains unchecked.
