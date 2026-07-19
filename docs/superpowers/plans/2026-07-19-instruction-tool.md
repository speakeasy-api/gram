# Instruction Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a synthetic `instructions` tool to every Gram-hosted/proxied MCP server that returns `mcp_metadata.instructions`, and (by default) gate each MCP session so the first tool call must read the instructions.

**Architecture:** A gateway-level synthetic tool following the existing `search_tools`/`describe_tools` pattern: injected into `tools/list` after assembly, intercepted in `handleToolsCall` before proxy/static resolution. Read-state lives in Redis via the existing `cache.TypedCacheObject` pattern, keyed by toolset + MCP session ID. One new enum column `mcp_metadata.instruction_tool_mode` (`disabled`/`optional`/`required`, default `required`) controls it; the gate arms only when instructions are non-empty.

**Tech Stack:** Go (Goa v3 design-first RPC, sqlc, atlas migrations, pgx), Redis via `internal/cache`, React/TypeScript dashboard, mise task runner.

**Spec:** `docs/superpowers/specs/2026-07-19-instruction-tool-design.md` (approved).

## Global Constraints

- All work happens in `/Users/sagar/go/src/gram` on a feature branch. Before Task 1: `git checkout -b feat/instruction-tool` (do NOT commit to the currently checked-out branch).
- Never hand-edit files under `server/migrations/` or `server/gen/` — they are generated (atlas / sqlc / Goa). Edit the sources (`server/database/schema.sql`, `**/queries.sql`, `server/design/**`) and run the mise gen tasks.
- No `CHECK` constraints for enum validation (repo Postgres rules) — validate mode values in application code / Goa `Enum`.
- Tool name is exactly `instructions`. Mode values are exactly `disabled`, `optional`, `required`. Default is `required`. Empty/unknown stored values parse as `required`.
- Instructions must never break tool serving: every metadata/Redis failure in the serving path logs and fails open.
- The gate response is a **successful** tool result (`isError` unset), never a JSON-RPC error.
- Empty-state message (exact copy): `No instructions have been configured for this server. An administrator can add them in the Gram dashboard under Server Instructions.`
- Local infra (Postgres/Redis/ClickHouse/Temporal) must be up for tests and migrations: run `./zero --agent` once if `mise run test:server` fails to connect.
- Go tests run from the repo root as `mise run test:server <go-test-args>` (task runs in `server/`), e.g. `mise run test:server ./internal/mcp/ -run TestServePublic_InstructionGate -v`.

---

### Task 1: Database column + sqlc queries

**Files:**
- Modify: `server/database/schema.sql` (~line 3250, `mcp_metadata` table)
- Modify: `server/internal/mcpmetadata/queries.sql`
- Generated (do not hand-edit): `server/migrations/<timestamp>_add-instruction-tool-mode-to-mcp-metadata.sql`, `server/migrations/atlas.sum`, `server/internal/mcpmetadata/repo/*`

**Interfaces:**
- Consumes: nothing (first task)
- Produces: `repo.McpMetadatum.InstructionToolMode string`; `repo.UpsertMetadataParams.InstructionToolMode string`; `repo.UpsertMetadataByMcpServerIDParams.InstructionToolMode string` (used by Tasks 2, 4, 5)

- [ ] **Step 1: Add the column to the schema**

In `server/database/schema.sql`, inside `CREATE TABLE IF NOT EXISTS mcp_metadata (...)`, after the `instructions TEXT,` line (line ~3250), add:

```sql
  instruction_tool_mode TEXT NOT NULL DEFAULT 'required', -- Synthetic instructions tool behavior: 'disabled' | 'optional' | 'required'. Validated in application code.
```

- [ ] **Step 2: Generate and apply the migration**

```bash
mise run db:diff add-instruction-tool-mode-to-mcp-metadata
mise run db:migrate
```

Expected: a new file appears in `server/migrations/` containing `ALTER TABLE "mcp_metadata" ADD COLUMN "instruction_tool_mode" text NOT NULL DEFAULT 'required'`, and migrate applies cleanly.

- [ ] **Step 3: Add the column to every metadata query**

In `server/internal/mcpmetadata/queries.sql`:
- Add `instruction_tool_mode,` to the SELECT column lists of `GetMetadataForToolset` (after `instructions,` line 9) and `GetMetadataByMcpServerID` (after line 28).
- In `UpsertMetadata`: add `instruction_tool_mode` to the INSERT column list (after `instructions`), add `@instruction_tool_mode` to VALUES (matching position), add `instruction_tool_mode = EXCLUDED.instruction_tool_mode,` to `DO UPDATE SET`, and add `instruction_tool_mode,` to RETURNING (after `instructions,`).
- Same four edits in `UpsertMetadataByMcpServerID`.

- [ ] **Step 4: Regenerate sqlc code and verify build**

```bash
mise run gen:sqlc-server
cd server && mise exec -- go build ./... && cd ..
```

Expected: build succeeds. Existing `UpsertMetadataParams{...}` literals compile unchanged (the new field defaults to zero value `""`; runtime parsing in Task 3 treats `""` as `required`).

- [ ] **Step 5: Run the existing metadata tests**

```bash
mise run test:server ./internal/mcpmetadata/ -count=1
```

Expected: PASS (no behavior change yet).

- [ ] **Step 6: Commit**

```bash
git add server/database/schema.sql server/migrations server/internal/mcpmetadata/queries.sql server/internal/mcpmetadata/repo
git commit -m "feat(mcp): add instruction_tool_mode column to mcp_metadata"
```

---

### Task 2: mcpMetadata API surface (Goa design + service impl)

**Files:**
- Modify: `server/design/mcpmetadata/design.go` (McpMetadata type ~line 107; setMcpMetadata payload ~line 187)
- Modify: `server/internal/mcpmetadata/impl.go` (SetMcpMetadata ~line 389-448; ToMCPMetadata ~line 833)
- Test: `server/internal/mcpmetadata/setmetadata_test.go`
- Generated: `server/gen/**` (Goa), TS SDK via `mise run gen:sdk`

**Interfaces:**
- Consumes: Task 1 repo fields (`InstructionToolMode string` on params/row)
- Produces: `types.McpMetadata.InstructionToolMode *string`; `gen.SetMcpMetadataPayload.InstructionToolMode *string`; `@gram/client` `McpMetadata.instructionToolMode?: string` (used by Task 6)

- [ ] **Step 1: Write the failing test**

Append to `server/internal/mcpmetadata/setmetadata_test.go` (inside the file, as a new top-level test; reuse the existing imports and `createTestToolset` helper):

```go
func TestService_SetMcpMetadata_InstructionToolMode(t *testing.T) {
	t.Parallel()

	t.Run("round-trips instruction tool mode", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolset := createTestToolset(t, ctx, ti, "test-instruction-mode")

		result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
			ToolsetSlug:         conv.PtrEmpty(types.Slug(toolset.Slug)),
			Instructions:        new("Always verify the customer record after writes."),
			InstructionToolMode: new("optional"),
		})
		require.NoError(t, err)
		require.NotNil(t, result.InstructionToolMode)
		require.Equal(t, "optional", *result.InstructionToolMode)
	})

	t.Run("defaults to required when omitted", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolset := createTestToolset(t, ctx, ti, "test-instruction-mode-default")

		result, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
			ToolsetSlug: conv.PtrEmpty(types.Slug(toolset.Slug)),
		})
		require.NoError(t, err)
		require.NotNil(t, result.InstructionToolMode)
		require.Equal(t, "required", *result.InstructionToolMode)
	})

	t.Run("rejects invalid mode", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestMCPMetadataService(t)
		toolset := createTestToolset(t, ctx, ti, "test-instruction-mode-invalid")

		_, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
			ToolsetSlug:         conv.PtrEmpty(types.Slug(toolset.Slug)),
			InstructionToolMode: new("sometimes"),
		})
		require.Error(t, err)
	})
}
```

Match the existing payload-literal style in this file (it sets unused pointer fields to `nil` explicitly if the linter requires exhaustive struct literals — mirror whatever neighboring tests do).

- [ ] **Step 2: Run the test to verify it fails**

```bash
mise run test:server ./internal/mcpmetadata/ -run TestService_SetMcpMetadata_InstructionToolMode -v
```

Expected: FAIL — compile error, `InstructionToolMode` undefined on the payload.

- [ ] **Step 3: Add the attribute to the Goa design**

In `server/design/mcpmetadata/design.go`:

After the `instructions` attribute of the `McpMetadata` type (line ~107), add:

```go
	Attribute("instruction_tool_mode", String, "Behavior of the synthetic instructions tool on this MCP server. 'required' (default) gates each session on reading instructions, 'optional' lists the tool without gating, 'disabled' hides it.", func() {
		Enum("disabled", "optional", "required")
	})
```

After the `instructions` attribute in the `setMcpMetadata` Payload (line ~187), add the identical `Attribute("instruction_tool_mode", ...)` block.

- [ ] **Step 4: Regenerate Goa code and the SDK**

```bash
mise run gen:goa-server
mise run gen:sdk
```

Expected: `gen.SetMcpMetadataPayload` and `types.McpMetadata` gain `InstructionToolMode *string`; the TS client model `McpMetadata` gains `instructionToolMode`.

- [ ] **Step 5: Wire the field through the service impl**

In `server/internal/mcpmetadata/impl.go`, in `SetMcpMetadata` after the `instructions` block (line ~392), add:

```go
	instructionToolMode := "required"
	if payload.InstructionToolMode != nil {
		instructionToolMode = *payload.InstructionToolMode
	}
```

(Goa's `Enum` validation already rejects values outside the three allowed strings before the impl runs.)

Add `InstructionToolMode: instructionToolMode,` to both upsert param literals (`UpsertMetadataParams` line ~428 and `UpsertMetadataByMcpServerIDParams` line ~439).

In `ToMCPMetadata` (line ~824), add to the `types.McpMetadata` literal:

```go
		InstructionToolMode:       conv.PtrEmpty(record.InstructionToolMode),
```

- [ ] **Step 6: Run the test to verify it passes**

```bash
mise run test:server ./internal/mcpmetadata/ -run TestService_SetMcpMetadata_InstructionToolMode -v
mise run test:server ./internal/mcpmetadata/ -count=1
```

Expected: PASS, including all pre-existing metadata tests.

- [ ] **Step 7: Commit**

```bash
git add server/design/mcpmetadata/design.go server/internal/mcpmetadata/impl.go server/internal/mcpmetadata/setmetadata_test.go server/gen client
git commit -m "feat(mcp): expose instruction_tool_mode on mcpMetadata get/set"
```

(`git add client` picks up the regenerated TS SDK; if `mise run gen:sdk` writes elsewhere, add that path instead — check `git status`.)

---

### Task 3: Instruction tool core (`instruction_tool.go`)

**Files:**
- Create: `server/internal/mcp/instruction_tool.go`
- Test: `server/internal/mcp/instruction_tool_test.go`

**Interfaces:**
- Consumes: `toolListEntry` (rpc_tools_list.go:39), `externalmcp.ToolAnnotations`, `mcpmetadata_repo.Queries.GetMetadataForToolset`, `cache.CacheableObject`
- Produces (used by Tasks 4–5):
  - `const instructionsToolName = "instructions"`
  - `type instructionToolMode string` with consts `instructionToolModeDisabled`, `instructionToolModeOptional`, `instructionToolModeRequired`
  - `parseInstructionToolMode(string) instructionToolMode`
  - `type instructionToolConfig struct { Mode instructionToolMode; Instructions string }`
  - `fetchInstructionToolConfig(ctx context.Context, logger *slog.Logger, metadataRepo *mcpmetadata_repo.Queries, toolsetID uuid.UUID) instructionToolConfig`
  - `buildInstructionToolEntry() *toolListEntry`
  - `injectInstructionTool(entries []*toolListEntry, toolsetTools []*types.Tool, mode instructionToolMode) []*toolListEntry`
  - `type instructionSessionGate struct { ToolsetID, SessionID string }` implementing `cache.CacheableObject[instructionSessionGate]`
  - `instructionGateCacheKey(toolsetID, sessionID string) string`
  - `toolsetExposesInstructionsTool(tools []*types.Tool) bool`
  - `const instructionsNotConfiguredMessage`

- [ ] **Step 1: Write the failing unit tests**

Create `server/internal/mcp/instruction_tool_test.go`:

```go
package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseInstructionToolMode(t *testing.T) {
	t.Parallel()

	require.Equal(t, instructionToolModeDisabled, parseInstructionToolMode("disabled"))
	require.Equal(t, instructionToolModeOptional, parseInstructionToolMode("optional"))
	require.Equal(t, instructionToolModeRequired, parseInstructionToolMode("required"))
	// Legacy rows and unknown values fail safe to the default.
	require.Equal(t, instructionToolModeRequired, parseInstructionToolMode(""))
	require.Equal(t, instructionToolModeRequired, parseInstructionToolMode("bogus"))
}

func TestInjectInstructionTool(t *testing.T) {
	t.Parallel()

	entries := []*toolListEntry{{Name: "create_ticket"}, {Name: "list_tickets"}}

	t.Run("prepends the tool first", func(t *testing.T) {
		t.Parallel()
		out := injectInstructionTool(entries, nil, instructionToolModeRequired)
		require.Len(t, out, 3)
		require.Equal(t, instructionsToolName, out[0].Name)
		require.Equal(t, "create_ticket", out[1].Name)
	})

	t.Run("disabled mode injects nothing", func(t *testing.T) {
		t.Parallel()
		out := injectInstructionTool(entries, nil, instructionToolModeDisabled)
		require.Len(t, out, 2)
	})

	t.Run("skips on name collision with a listed tool", func(t *testing.T) {
		t.Parallel()
		colliding := append([]*toolListEntry{{Name: instructionsToolName}}, entries...)
		out := injectInstructionTool(colliding, nil, instructionToolModeRequired)
		require.Len(t, out, 3)
	})

	t.Run("optional mode still injects", func(t *testing.T) {
		t.Parallel()
		out := injectInstructionTool(entries, nil, instructionToolModeOptional)
		require.Equal(t, instructionsToolName, out[0].Name)
	})
}

func TestInstructionSessionGateCacheKey(t *testing.T) {
	t.Parallel()

	g := instructionSessionGate{ToolsetID: "ts-1", SessionID: "sess-9"}
	require.Equal(t, "mcpInstructionsRead:ts-1:sess-9", g.CacheKey())
	require.Equal(t, g.CacheKey(), instructionGateCacheKey("ts-1", "sess-9"))
	require.Empty(t, g.AdditionalCacheKeys())
	require.Equal(t, 60*time.Minute, g.TTL())
}
```

Add `"time"` to the imports.

- [ ] **Step 2: Run tests to verify they fail**

```bash
mise run test:server ./internal/mcp/ -run 'TestParseInstructionToolMode|TestInjectInstructionTool|TestInstructionSessionGateCacheKey' -v
```

Expected: FAIL — compile errors, symbols undefined.

- [ ] **Step 3: Implement `instruction_tool.go`**

Create `server/internal/mcp/instruction_tool.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
)

// The synthetic instruction tool surfaces mcp_metadata.instructions as a
// callable tool on every Gram MCP server, because many MCP clients ignore the
// instructions field of the initialize response. In "required" mode (the
// default) tools/call gates each MCP session on reading the instructions
// before any other tool runs.
const instructionsToolName = "instructions"

const instructionsToolDescription = "Server usage guide for this MCP server. Returns organization-specific conventions, required workflows, and verification steps for using the other tools. Call this once before using any other tool."

const instructionsNotConfiguredMessage = "No instructions have been configured for this server. An administrator can add them in the Gram dashboard under Server Instructions."

type instructionToolMode string

const (
	instructionToolModeDisabled instructionToolMode = "disabled"
	instructionToolModeOptional instructionToolMode = "optional"
	instructionToolModeRequired instructionToolMode = "required"
)

// parseInstructionToolMode maps a stored mode value to a known mode. Empty
// (legacy rows written before the column's application-side default) and
// unknown values fail safe to required.
func parseInstructionToolMode(raw string) instructionToolMode {
	switch instructionToolMode(raw) {
	case instructionToolModeDisabled, instructionToolModeOptional:
		return instructionToolMode(raw)
	default:
		return instructionToolModeRequired
	}
}

type instructionToolConfig struct {
	Mode         instructionToolMode
	Instructions string
}

// fetchInstructionToolConfig loads the instruction tool settings for a
// toolset. Lookup failures fail open: the tool stays listed (mode required)
// but with no instructions content, which also keeps the gate disarmed.
func fetchInstructionToolConfig(ctx context.Context, logger *slog.Logger, metadataRepo *mcpmetadata_repo.Queries, toolsetID uuid.UUID) instructionToolConfig {
	fallback := instructionToolConfig{Mode: instructionToolModeRequired, Instructions: ""}

	metadata, err := metadataRepo.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: toolsetID, Valid: true})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.WarnContext(ctx, "failed to fetch MCP metadata for instruction tool", attr.SlogError(err))
		}
		return fallback
	}

	cfg := instructionToolConfig{
		Mode:         parseInstructionToolMode(metadata.InstructionToolMode),
		Instructions: "",
	}
	if metadata.Instructions.Valid {
		cfg.Instructions = metadata.Instructions.String
	}
	return cfg
}

var instructionToolInputSchema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

func buildInstructionToolEntry() *toolListEntry {
	readOnly := true
	idempotent := true
	return &toolListEntry{
		Name:        instructionsToolName,
		Description: instructionsToolDescription,
		InputSchema: instructionToolInputSchema,
		Annotations: &externalmcp.ToolAnnotations{
			ReadOnlyHint:   &readOnly,
			IdempotentHint: &idempotent,
		},
		Meta: nil,
	}
}

// injectInstructionTool prepends the synthetic instruction tool to a built
// tools/list, unless the mode disables it or a real tool already claims the
// name (Gram never shadows a customer tool). entries covers tools already
// materialized for the response (including live-listed proxy tools);
// toolsetTools additionally covers underlying tools not present in entries,
// e.g. real tools reachable via execute_tool in dynamic mode.
func injectInstructionTool(entries []*toolListEntry, toolsetTools []*types.Tool, mode instructionToolMode) []*toolListEntry {
	if mode == instructionToolModeDisabled {
		return entries
	}
	for _, e := range entries {
		if e.Name == instructionsToolName {
			return entries
		}
	}
	if toolsetExposesInstructionsTool(toolsetTools) {
		return entries
	}
	return append([]*toolListEntry{buildInstructionToolEntry()}, entries...)
}

// toolsetExposesInstructionsTool reports whether a materialized (non-proxy)
// tool in the toolset is named "instructions". Proxy upstreams are not
// live-listed here; a proxy tool with this name is caught by the entries
// check in injectInstructionTool on the list path. On the call path this is
// the only collision guard — a known limitation for proxy upstreams.
func toolsetExposesInstructionsTool(tools []*types.Tool) bool {
	for _, t := range tools {
		if conv.IsProxyTool(t) {
			continue
		}
		baseTool, err := conv.ToBaseTool(t)
		if err != nil {
			continue
		}
		if baseTool.Name == instructionsToolName {
			return true
		}
	}
	return false
}

// instructionSessionGate marks that an MCP session has read the server
// instructions. Stored in Redis under mcpInstructionsRead:{toolset}:{session}
// for 60 minutes; re-Stored on each gated tools/call so active sessions do
// not expire mid-use.
type instructionSessionGate struct {
	ToolsetID string `json:"toolset_id"`
	SessionID string `json:"session_id"`
}

var _ cache.CacheableObject[instructionSessionGate] = (*instructionSessionGate)(nil)

func instructionGateCacheKey(toolsetID, sessionID string) string {
	return "mcpInstructionsRead:" + toolsetID + ":" + sessionID
}

// CacheKey implements cache.CacheableObject.
func (g instructionSessionGate) CacheKey() string {
	return instructionGateCacheKey(g.ToolsetID, g.SessionID)
}

// AdditionalCacheKeys implements cache.CacheableObject.
func (g instructionSessionGate) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject.
func (g instructionSessionGate) TTL() time.Duration { return 60 * time.Minute }
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
mise run test:server ./internal/mcp/ -run 'TestParseInstructionToolMode|TestInjectInstructionTool|TestInstructionSessionGateCacheKey' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/mcp/instruction_tool.go server/internal/mcp/instruction_tool_test.go
git commit -m "feat(mcp): instruction tool core - entry, mode config, session gate object"
```

---

### Task 4: tools/list injection

**Files:**
- Modify: `server/internal/mcp/rpc_tools_list.go` (`handleToolsList`, after the RBAC filter block ending line ~135)
- Modify: `server/internal/mcp/impl.go` (dispatch line ~1115)
- Test: `server/internal/mcp/servepublic_test.go`

**Interfaces:**
- Consumes: Task 3 (`fetchInstructionToolConfig`, `injectInstructionTool`), `s.mcpMetadataRepo` (already a Service field, used at impl.go:1111)
- Produces: `handleToolsList` gains trailing param `mcpMetadataRepo *mcpmetadata_repo.Queries`; the `instructions` tool appears first in tools/list (Tasks 5–6 rely on this behavior)

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/mcp/servepublic_test.go` (reuse the file's existing imports; `makeToolsListBody()` is defined in `rpc_tools_list_test.go`, same package). Also add this local helper next to the new tests — a public toolset + metadata fixture copied from `TestServePublic_ServerInstructionsInInitializeResponse` (line ~401):

```go
// createInstructionToolset creates a public MCP toolset with the given
// instructions + instruction tool mode and returns its mcp slug.
func createInstructionToolset(t *testing.T, ctx context.Context, ti *testInstance, slug, instructions, mode string) string {
	t.Helper()

	toolsetsRepo := toolsets_repo.New(ti.conn)
	metadataRepo := metadata_repo.New(ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Instruction tool test server " + slug,
		Slug:                   slug,
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	_, err = metadataRepo.UpsertMetadata(ctx, metadata_repo.UpsertMetadataParams{
		ToolsetID:                uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ProjectID:                *authCtx.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{String: "", Valid: false},
		LogoID:                   uuid.NullUUID{Valid: false},
		Instructions:             conv.ToPGText(instructions),
		InstructionToolMode:      mode,
	})
	require.NoError(t, err)

	return toolset.McpSlug.String
}

// listToolNames runs tools/list against the public endpoint and returns the
// tool names in order.
func listToolNames(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug string) []string {
	t.Helper()

	w, err := servePublicHTTP(t, ctx, ti, mcpSlug, makeToolsListBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "response body: %s", w.Body.String())

	names := make([]string, 0, len(response.Result.Tools))
	for _, tool := range response.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestServePublic_InstructionToolListedFirst(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-list-required", "Verify the customer record after every write.", "required")

	names := listToolNames(t, ctx, ti, mcpSlug)
	require.NotEmpty(t, names)
	require.Equal(t, "instructions", names[0])
}

func TestServePublic_InstructionToolListedInOptionalMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-list-optional", "Some instructions.", "optional")

	names := listToolNames(t, ctx, ti, mcpSlug)
	require.Contains(t, names, "instructions")
}

func TestServePublic_InstructionToolHiddenWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-list-disabled", "Some instructions.", "disabled")

	names := listToolNames(t, ctx, ti, mcpSlug)
	require.NotContains(t, names, "instructions")
}

func TestServePublic_InstructionToolListedWithoutMetadataRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Instruction tool no metadata",
		Slug:                   "instr-list-no-metadata",
		Description:            conv.ToPGText("A test MCP server"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText("instr-list-no-metadata"),
		McpEnabled:             true,
	})
	require.NoError(t, err)
	_, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	names := listToolNames(t, ctx, ti, "instr-list-no-metadata")
	require.Equal(t, []string{"instructions"}, names)
}
```

If any import (`pgtype`, `metadata_repo`, etc.) is missing from `servepublic_test.go`, add it — `TestServePublic_ServerInstructionsInInitializeResponse` already uses them all.

- [ ] **Step 2: Run tests to verify they fail**

```bash
mise run test:server ./internal/mcp/ -run 'TestServePublic_InstructionTool' -v
```

Expected: FAIL — `names[0]` is not `instructions` (tool absent) or empty tools list assertions fail. (The `InstructionToolMode` field on `UpsertMetadataParams` compiles thanks to Task 1.)

- [ ] **Step 3: Inject the tool in handleToolsList**

In `server/internal/mcp/rpc_tools_list.go`:

Add to imports: `mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"`.

Add a trailing parameter to `handleToolsList` (after `platformExtras []platformtools.ExternalTool,`):

```go
	mcpMetadataRepo *mcpmetadata_repo.Queries,
```

After the RBAC filter block (the `tools = allowed` close at line ~135) and BEFORE the shadowmcp block, insert:

```go
	// Inject the synthetic instruction tool. Placed after the RBAC filter on
	// purpose: the tool is gateway-provided, carries no customer data beyond
	// the instructions text, and is not subject to per-tool grants. Placed
	// before shadowmcp injection so its schema gets the same toolset-id
	// constant as every other tool.
	if toolsetUUID, err := uuid.Parse(toolset.ID); err != nil {
		logger.WarnContext(ctx, "invalid toolset id; skipping instruction tool injection", attr.SlogError(err))
	} else {
		itCfg := fetchInstructionToolConfig(ctx, logger, mcpMetadataRepo, toolsetUUID)
		tools = injectInstructionTool(tools, toolset.Tools, itCfg.Mode)
	}
```

In `server/internal/mcp/impl.go` line ~1115, update the dispatch call to pass the repo:

```go
	case "tools/list":
		return handleToolsList(ctx, s.logger, s.authz, s.guardianPolicy, s.db, s.env, payload, req, s.posthog, &s.toolsetCache, s.vectorToolStore, s.temporal, s.shadowMCPClient, s.platformExtras, s.mcpMetadataRepo)
```

Fix any other `handleToolsList` call sites the compiler reports (grep: `grep -rn "handleToolsList(" server/internal/mcp/`).

- [ ] **Step 4: Run tests to verify they pass**

```bash
mise run test:server ./internal/mcp/ -run 'TestServePublic_InstructionTool' -v
mise run test:server ./internal/mcp/ -run 'TestServePublic|TestHandleToolsList' -count=1
```

Expected: new tests PASS; pre-existing tools/list tests still PASS. If an existing test asserts an exact tool list, update it to account for the prepended `instructions` entry — that behavior change is intended.

- [ ] **Step 5: Commit**

```bash
git add server/internal/mcp/rpc_tools_list.go server/internal/mcp/impl.go server/internal/mcp/servepublic_test.go
git commit -m "feat(mcp): inject synthetic instructions tool into tools/list"
```

---

### Task 5: tools/call interception + session gate

**Files:**
- Modify: `server/internal/mcp/impl.go` (`parseMcpSessionID` ~line 1131, `mcpInputs` ~line 204, request build ~line 803/826, Service struct ~line 125, NewService ~line 332, dispatch ~line 1117)
- Modify: `server/internal/mcp/rpc_tools_call.go` (`handleToolsCall`, after toolsetID parse ~line 148)
- Modify: `server/internal/mcp/instruction_tool.go` (add call handlers)
- Test: `server/internal/mcp/servepublic_test.go`

**Interfaces:**
- Consumes: Task 3 symbols; Task 4 fixture helpers (`createInstructionToolset`); `makeToolsCallBody(toolName)` (servepublic_test.go:514); `rediscache "github.com/go-redis/cache/v9"` (`ErrCacheMiss` — how `internal/cache` surfaces misses, see `server/internal/cache/redis.go:47`)
- Produces: `payload.sessionProvided bool`; Service field `instructionGateCache cache.TypedCacheObject[instructionSessionGate]`; `handleToolsCall` gains trailing params `productMetrics *posthog.Posthog, instructionGateCache *cache.TypedCacheObject[instructionSessionGate]`

- [ ] **Step 1: Write the failing tests**

Append to `server/internal/mcp/servepublic_test.go`:

```go
// callTool posts a tools/call for toolName with an optional Mcp-Session-Id
// header and returns the raw response body.
func callTool(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug, toolName, sessionID string) string {
	t.Helper()

	headers := map[string]string{}
	if sessionID != "" {
		headers["Mcp-Session-Id"] = sessionID
	}
	w, _ := servePublicHTTP(t, ctx, ti, mcpSlug, makeToolsCallBody(toolName), "", headers)
	return w.Body.String()
}

func TestServePublic_InstructionsToolCallReturnsText(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	instructions := "Always verify the customer record changed after each write."
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-call-text", instructions, "required")

	body := callTool(t, ctx, ti, mcpSlug, "instructions", "")
	require.Contains(t, body, instructions)
	require.NotContains(t, body, `"isError":true`)
}

func TestServePublic_InstructionsToolCallEmptyState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-call-empty", "", "required")

	body := callTool(t, ctx, ti, mcpSlug, "instructions", "")
	require.Contains(t, body, "No instructions have been configured for this server.")
}

func TestServePublic_InstructionGateBlocksFirstCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	instructions := "Check the customer was changed before reporting success."
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-gate-blocks", instructions, "required")

	// First call in the session is not the instructions tool: Gram must NOT
	// execute it, and must return the instructions with a retry note.
	first := callTool(t, ctx, ti, mcpSlug, "some_other_tool", "gate-session-1")
	require.Contains(t, first, instructions)
	require.Contains(t, first, "retry your original call")

	// The gate response itself delivered the instructions, so the session is
	// now marked as read: the retry proceeds to normal resolution (here:
	// tool not found, since the toolset has no real tools).
	second := callTool(t, ctx, ti, mcpSlug, "some_other_tool", "gate-session-1")
	require.NotContains(t, second, "retry your original call")
}

func TestServePublic_InstructionGateSkippedWithoutSessionHeader(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-gate-nosession", "Some instructions.", "required")

	// Clients that never send Mcp-Session-Id get a fresh ID per request and
	// could never pass a session-keyed gate — the gate must fail open.
	body := callTool(t, ctx, ti, mcpSlug, "some_other_tool", "")
	require.NotContains(t, body, "retry your original call")
}

func TestServePublic_InstructionGateSkippedInOptionalMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-gate-optional", "Some instructions.", "optional")

	body := callTool(t, ctx, ti, mcpSlug, "some_other_tool", "opt-session-1")
	require.NotContains(t, body, "retry your original call")
}

func TestServePublic_InstructionGateSkippedWhenInstructionsEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-gate-empty", "", "required")

	body := callTool(t, ctx, ti, mcpSlug, "some_other_tool", "empty-session-1")
	require.NotContains(t, body, "retry your original call")
}

func TestServePublic_InstructionsCallOpensGate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	instructions := "Read me first."
	mcpSlug := createInstructionToolset(t, ctx, ti, "instr-gate-read-first", instructions, "required")

	first := callTool(t, ctx, ti, mcpSlug, "instructions", "polite-session-1")
	require.Contains(t, first, instructions)

	second := callTool(t, ctx, ti, mcpSlug, "some_other_tool", "polite-session-1")
	require.NotContains(t, second, "retry your original call")
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
mise run test:server ./internal/mcp/ -run 'TestServePublic_Instruction' -v
```

Expected: the Task 4 list tests still PASS; the new gate/call tests FAIL (calling `instructions` returns tool-not-found; gate never fires).

- [ ] **Step 3: Track whether the client sent a session ID**

In `server/internal/mcp/impl.go`:

Change `parseMcpSessionID` (line ~1131) to:

```go
// parseMcpSessionID returns the MCP session ID and whether the client
// actually sent one. When absent, a fresh UUID is minted per request — such
// requests have no cross-request continuity, so session-keyed features (the
// instruction gate) must treat provided=false as "no session".
func parseMcpSessionID(headers http.Header) (string, bool) {
	session := headers.Get("Mcp-Session-Id")
	if session == "" {
		return uuid.New().String(), false
	}
	return session, true
}
```

Update the call site (line ~803): `sessionID, sessionProvided := parseMcpSessionID(r.Header)`.

Add to `mcpInputs` (after `sessionID string`, line ~211):

```go
	// sessionProvided is true when the client sent an Mcp-Session-Id header
	// (vs Gram minting a throwaway ID for this request). Session-keyed
	// features must no-op when false.
	sessionProvided bool
```

Set `sessionProvided: sessionProvided,` in the `&mcpInputs{...}` literal (line ~819). Then `grep -n "mcpInputs{" server/internal/mcp/*.go` and set `sessionProvided: false` explicitly at every other construction site (internal agent-workflow callers synthesize their own session IDs — the gate must stay off for them).

Add the Service field (after `userSessionGrantCache`, line ~125):

```go
	instructionGateCache cache.TypedCacheObject[instructionSessionGate]
```

Construct it in `NewService` (after `userSessionGrantCache`, line ~332):

```go
		instructionGateCache: cache.NewTypedObjectCache[instructionSessionGate](
			logger.With(attr.SlogCacheNamespace("mcp_instruction_gate")),
			cacheImpl,
			cache.SuffixNone,
		),
```

Update the `tools/call` dispatch (line ~1117) to append `s.posthog, &s.instructionGateCache`:

```go
	case "tools/call":
		return handleToolsCall(ctx, s.logger, s.metrics, s.authz, s.guardianPolicy, s.db, s.env, payload, req, s.toolProxy, s.billingTracker, s.billingRepository, &s.toolsetCache, s.telemLogger, s.vectorToolStore, s.temporal, s.mcpMetadataRepo, s.auditLogger, s.platformExtras, s.posthog, &s.instructionGateCache)
```

- [ ] **Step 4: Add the call handlers to instruction_tool.go**

Append to `server/internal/mcp/instruction_tool.go` (extend imports with `"github.com/speakeasy-api/gram/server/internal/oops"` and `"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"`; the `rediscache` import belongs in `rpc_tools_call.go`, Step 5):

```go
// handleInstructionsToolCall serves a call to the synthetic instructions
// tool: returns the configured instructions (or the not-configured message)
// and marks the session as having read them.
func handleInstructionsToolCall(
	ctx context.Context,
	logger *slog.Logger,
	reqID json.RawMessage,
	payload *mcpInputs,
	toolsetID uuid.UUID,
	cfg instructionToolConfig,
	gateCache *cache.TypedCacheObject[instructionSessionGate],
) (json.RawMessage, error) {
	markInstructionsRead(ctx, logger, gateCache, toolsetID, payload)

	text := cfg.Instructions
	if text == "" {
		text = instructionsNotConfiguredMessage
	}
	return buildInstructionsTextResult(ctx, logger, reqID, text)
}

// buildInstructionGateResponse answers a gated tools/call: the blocked tool
// is NOT executed; the agent receives the instructions plus a retry note as
// a successful result (agents handle content responses more predictably
// than JSON-RPC errors, and the round trip cost stays at exactly one).
func buildInstructionGateResponse(
	ctx context.Context,
	logger *slog.Logger,
	reqID json.RawMessage,
	instructions string,
	blockedTool string,
) (json.RawMessage, error) {
	text := instructions + "\n\n---\nThis MCP server requires reading the server instructions (above) before other tools run. Your call to \"" + blockedTool + "\" was not executed. Retry it now."
	return buildInstructionsTextResult(ctx, logger, reqID, text)
}

func buildInstructionsTextResult(ctx context.Context, logger *slog.Logger, reqID json.RawMessage, text string) (json.RawMessage, error) {
	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     text,
		MimeType: nil,
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize instructions chunk").LogError(ctx, logger)
	}

	response, err := json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content:           []json.RawMessage{chunk},
			StructuredContent: nil,
			IsError:           false,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize instructions response").LogError(ctx, logger)
	}
	return response, nil
}

// markInstructionsRead stores (or refreshes) the session gate flag. Redis
// failures are logged and ignored — the gate fails open, never breaking a
// tool call.
func markInstructionsRead(ctx context.Context, logger *slog.Logger, gateCache *cache.TypedCacheObject[instructionSessionGate], toolsetID uuid.UUID, payload *mcpInputs) {
	if !payload.sessionProvided {
		return
	}
	if err := gateCache.Store(ctx, instructionSessionGate{ToolsetID: toolsetID.String(), SessionID: payload.sessionID}); err != nil {
		logger.WarnContext(ctx, "failed to store instruction gate flag", attr.SlogError(err))
	}
}

// captureInstructionGateEvent emits the product metric for a gate trigger —
// the measure of how often agents would have skipped reading instructions.
func captureInstructionGateEvent(ctx context.Context, logger *slog.Logger, productMetrics *posthog.Posthog, payload *mcpInputs, toolsetSlug, toolsetID, blockedTool string) {
	if err := productMetrics.CaptureEvent(ctx, "mcp_instructions_gate_triggered", payload.sessionID, map[string]any{
		"project_id":           payload.projectID.String(),
		"toolset_slug":         toolsetSlug,
		"toolset_id":           toolsetID,
		"blocked_tool":         blockedTool,
		"mcp_session_id":       payload.sessionID,
		"disable_notification": true,
	}); err != nil {
		logger.ErrorContext(ctx, "failed to capture mcp_instructions_gate_triggered event", attr.SlogError(err))
	}
}
```

Note: `reqID`'s type must match `rawRequest.ID` (see `server/internal/mcp/rpc.go` and how `handleSearchToolsCall` receives `req.ID`). If it is not `json.RawMessage`, use that exact type in the three functions above instead.

- [ ] **Step 5: Intercept and gate in handleToolsCall**

In `server/internal/mcp/rpc_tools_call.go`:

Add to imports: `rediscache "github.com/go-redis/cache/v9"`, `"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"`.

Add two trailing parameters to `handleToolsCall` (after `platformExtras []platformtools.ExternalTool,`):

```go
	productMetrics *posthog.Posthog,
	instructionGateCache *cache.TypedCacheObject[instructionSessionGate],
```

Immediately after the `toolsetID, err := uuid.Parse(toolset.ID)` block (line ~145-148) — i.e., after the dynamic-mode switch has already unwrapped `execute_tool` into the real `params.Name`, and before `executor := externalmcp.BuildProxyToolExecutor(...)` — insert:

```go
	// Synthetic instruction tool: intercept its call and enforce the
	// read-before-use session gate. Skipped entirely when the toolset
	// materializes a real tool with the same name — Gram never shadows a
	// customer tool. search_tools/describe_tools returned above and are
	// deliberately ungated: they explore, they don't act.
	if !toolsetExposesInstructionsTool(toolset.Tools) {
		itCfg := fetchInstructionToolConfig(ctx, logger, mcpMetadataRepo, toolsetID)
		if params.Name == instructionsToolName {
			if itCfg.Mode == instructionToolModeDisabled {
				return nil, oops.E(oops.CodeNotFound, errors.New("tool not found"), "tool not found").LogError(ctx, logger)
			}
			return handleInstructionsToolCall(ctx, logger, req.ID, payload, toolsetID, itCfg, instructionGateCache)
		}
		if itCfg.Mode == instructionToolModeRequired && itCfg.Instructions != "" && payload.sessionProvided {
			_, gateErr := instructionGateCache.Get(ctx, instructionGateCacheKey(toolsetID.String(), payload.sessionID))
			switch {
			case gateErr == nil:
				// Session already read the instructions; refresh the TTL so
				// active sessions don't expire mid-use.
				markInstructionsRead(ctx, logger, instructionGateCache, toolsetID, payload)
			case errors.Is(gateErr, rediscache.ErrCacheMiss):
				captureInstructionGateEvent(ctx, logger, productMetrics, payload, toolset.Slug, toolset.ID, params.Name)
				// The gate response carries the full instructions, so this
				// counts as the read: open the gate for the retry.
				markInstructionsRead(ctx, logger, instructionGateCache, toolsetID, payload)
				return buildInstructionGateResponse(ctx, logger, req.ID, itCfg.Instructions, params.Name)
			default:
				logger.WarnContext(ctx, "instruction gate lookup failed; failing open", attr.SlogError(gateErr))
			}
		}
	}
```

(`toolset.Slug` — confirm the field name on `*types.Toolset`; `handleToolsList` uses `toolset.Slug` in its PostHog event at rpc_tools_list.go:86.)

Fix any other `handleToolsCall` call sites the compiler reports (grep: `grep -rn "handleToolsCall(" server/internal/mcp/`), passing the service's posthog + gate cache (or, for internal agent-workflow callers, the same fields from their service instance).

- [ ] **Step 6: Run tests to verify they pass**

```bash
mise run test:server ./internal/mcp/ -run 'TestServePublic_Instruction' -v
```

Expected: all instruction tests PASS.

- [ ] **Step 7: Run the full mcp package suite**

```bash
mise run test:server ./internal/mcp/ -count=1
```

Expected: PASS. If a pre-existing tools/call test now hits the gate (it sends an `Mcp-Session-Id` header on a toolset with non-empty instructions), that test's fixture should set `InstructionToolMode: "optional"` — the behavior change is intended, choose the fixture change over weakening the gate.

- [ ] **Step 8: Commit**

```bash
git add server/internal/mcp
git commit -m "feat(mcp): gate MCP sessions on reading server instructions"
```

---

### Task 6: Dashboard mode control

**Files:**
- Modify: `client/dashboard/src/components/mcp_install_page/useMcpMetadataForm.tsx`
- Modify: `client/dashboard/src/pages/mcp/MCPDetails.tsx` (`ServerInstructionsSection`, line ~764)

**Interfaces:**
- Consumes: `@gram/client` `McpMetadata.instructionToolMode` (regenerated in Task 2); `UseMcpMetadataMetadataFormResult` (useMcpMetadataForm.tsx:43)
- Produces: `form.instructionToolModeHandlers: { value: string; onChange: (mode: string) => void }`

- [ ] **Step 1: Extend the form hook**

In `useMcpMetadataForm.tsx`:

1. Add to `MetadataParams` (line ~16): `instructionToolMode: string | undefined;`
2. Add to the initial `useState` object (line ~98) and to the server-sync `setMetadataParams` call (line ~147): `instructionToolMode: currentMetadata?.instructionToolMode ?? undefined,` (initial) / `instructionToolMode: currentMetadata?.instructionToolMode,` (sync).
3. Add to the `reset` callback object (line ~233) and `resetInstructions` (line ~253): `instructionToolMode: currentMetadata?.instructionToolMode,`.
4. Extend `instructionsDirty` (line ~219):

```ts
  const instructionsDirty = useMemo(() => {
    if (!currentMetadata) {
      return (
        metadataParams.instructions !== undefined ||
        metadataParams.instructionToolMode !== undefined
      );
    }
    return (
      metadataParams.instructions !== currentMetadata.instructions ||
      metadataParams.instructionToolMode !== currentMetadata.instructionToolMode
    );
  }, [
    currentMetadata,
    metadataParams.instructions,
    metadataParams.instructionToolMode,
  ]);
```

5. Add to the interface `UseMcpMetadataMetadataFormResult` (after `instructionsHandlers`, line ~67):

```ts
  instructionToolModeHandlers: {
    value: string;
    onChange: (mode: string) => void;
  };
```

6. Add to the returned object (after `instructionsHandlers`, line ~338):

```ts
    instructionToolModeHandlers: {
      value: metadataParams.instructionToolMode ?? "required",
      onChange: (mode: string) =>
        setMetadataParams((prev) => ({
          ...prev,
          instructionToolMode: mode,
        })),
    },
```

(`save`/`saveAsync` spread `metadataParams` into the request body, so the new field flows to `mcpMetadata.set` with no further changes.)

- [ ] **Step 2: Add the mode control to ServerInstructionsSection**

In `MCPDetails.tsx`, above `ServerInstructionsSection` (line ~764), add:

```tsx
const INSTRUCTION_TOOL_MODES = [
  {
    value: "required",
    label: "Required",
    hint: "Agents must read instructions before their first tool call in each session.",
  },
  {
    value: "optional",
    label: "Optional",
    hint: "The instructions tool is listed, but agents aren't required to call it.",
  },
  {
    value: "disabled",
    label: "Disabled",
    hint: "The instructions tool is not listed on this server.",
  },
] as const;
```

Inside `ServerInstructionsSection`, after the textarea `<div className="relative">...</div>` block (line ~799) and before the `{canWrite && (` save row, insert:

```tsx
      <Stack gap={1}>
        <Stack direction="horizontal" gap={1}>
          {INSTRUCTION_TOOL_MODES.map((mode) => (
            <Button
              key={mode.value}
              size="sm"
              variant={
                form.instructionToolModeHandlers.value === mode.value
                  ? "secondary"
                  : "ghost"
              }
              disabled={!canWrite}
              onClick={() => form.instructionToolModeHandlers.onChange(mode.value)}
            >
              <Button.Text>{mode.label}</Button.Text>
            </Button>
          ))}
        </Stack>
        <span className="text-muted-foreground text-xs">
          {
            INSTRUCTION_TOOL_MODES.find(
              (mode) => mode.value === form.instructionToolModeHandlers.value,
            )?.hint
          }
        </span>
      </Stack>
```

Match the file's existing `Button` variant names — if `"secondary"`/`"ghost"` aren't valid variants in this design system, use the nearest selected/unselected pair used elsewhere in the file. The existing Save button (wired to `form.saveAsync` + `form.instructionsDirty`) now also covers mode changes via the extended `instructionsDirty`.

- [ ] **Step 3: Typecheck and lint the dashboard**

```bash
mise run lint:client
```

Expected: PASS (no type errors on the new field — confirms the Task 2 SDK regen landed).

- [ ] **Step 4: Manual smoke test (optional but recommended)**

With local infra up (`./zero --agent`), open the dashboard MCP details page for any toolset, flip the mode control, Save, and confirm `mcpMetadata.get` returns the new mode after reload.

- [ ] **Step 5: Commit**

```bash
git add client/dashboard/src/components/mcp_install_page/useMcpMetadataForm.tsx client/dashboard/src/pages/mcp/MCPDetails.tsx
git commit -m "feat(dashboard): instruction tool mode control in Server Instructions"
```

---

### Task 7: Full verification

**Files:** none new — verification only.

- [ ] **Step 1: Full server test pass for the touched packages**

```bash
mise run test:server ./internal/mcp/ ./internal/mcpmetadata/ -count=1
```

Expected: PASS.

- [ ] **Step 2: Server lint + migration lint**

```bash
mise run lint:server
mise run lint:migrations
```

Expected: PASS.

- [ ] **Step 3: End-to-end check against a live local server**

Run `./zero --agent` (or `mise run start`), then against a local toolset with instructions configured and mode `required`:

```bash
# Initialize (returns a session id in the Mcp-Session-Id response header)
curl -sk https://localhost:8080/mcp/<mcp-slug> -H 'Content-Type: application/json' -D - \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'

# tools/list shows "instructions" first
curl -sk https://localhost:8080/mcp/<mcp-slug> -H 'Content-Type: application/json' -H 'Mcp-Session-Id: e2e-1' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'

# Calling any other tool first returns the gate response (instructions + retry note)
curl -sk https://localhost:8080/mcp/<mcp-slug> -H 'Content-Type: application/json' -H 'Mcp-Session-Id: e2e-1' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"<any-real-tool>","arguments":{}}}'

# The same call again in the same session now executes normally
curl -sk https://localhost:8080/mcp/<mcp-slug> -H 'Content-Type: application/json' -H 'Mcp-Session-Id: e2e-1' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"<any-real-tool>","arguments":{}}}'
```

Expected: list shows `instructions` first; third call returns the gate text without executing; fourth call executes the tool.

- [ ] **Step 4: Commit any remaining fixes and finish**

```bash
git status   # confirm clean
```

Use superpowers:finishing-a-development-branch to decide merge/PR next steps.
