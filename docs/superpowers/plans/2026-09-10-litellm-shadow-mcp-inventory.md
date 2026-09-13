# LiteLLM Shadow MCP Inventory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** MCP servers that a customer's LiteLLM proxy sees show up in Gram's Shadow MCP inventory with usage and per-user attribution, in both LiteLLM deployment modes.

**Architecture:** Gram's inventory is keyed end to end on a canonical server URL string: `telemetry_logs` attributes (`gram.mcp.server_url`) → `trace_summaries.mcp_server_url` (MV) → `shadow_mcp_inventory_urls` (usage backfill) → `access.listShadowMCPInventory` → dashboard. LiteLLM never reaches that chain today. This plan feeds it from two LiteLLM signals: (1) the Generic Guardrail payload, where MCP tools appear only as namespaced tool names (`mcp__<server>__<tool>`) or as Responses-API hosted-MCP tool entries carrying a `server_url`; (2) OTLP spans from the LiteLLM MCP gateway, which carry `metadata.mcp_tool_call_metadata` with the upstream server origin. Servers known only by name get a **synthetic identity URI** `mcp-tool://<server>` (helper already in `server/internal/shadowmcp/tool_namespace.go`), which passes through every URL-keyed layer with no schema change and is surfaced as a new `target_kind = "tool_namespace"` that is observe-only (review recorded, no enforcement written), mirroring stdio commands.

**Tech Stack:** Go (Goa design + generated stubs, ClickHouse via squirrel in `server/internal/telemetry/repo`), React dashboard with generated TypeScript SDK.

**Spec:** Findings and design are recorded on Linear DNO-1053 (comment dated 2026-09-10). Summary of the requirements:

- R1. A LiteLLM guardrail request whose `tools` array contains `mcp__<server>__<tool>` function tools produces an inventory row per distinct `<server>` for the project, keyed `mcp-tool://<server>`.
- R2. A LiteLLM guardrail request whose `tools` array contains `{type: "mcp", server_url, server_label}` entries produces a normal URL inventory row per `server_url`.
- R3. Each LiteLLM-observed MCP **tool call** (response `tool_calls` with an MCP-namespaced name) produces a `telemetry_logs` row whose `trace_id` is `sha256(tool_call_id)[:16]` and whose attributes carry `gram.mcp.server_url`, `gram.mcp.match`, `gram.tool_call.source`, `gram.hook.source = "litellm"`, and the user email, so `trace_summaries` yields usage, last-called, and per-user attribution for the row.
- R4. LiteLLM OTLP spans carrying `metadata.mcp_tool_call_metadata` are ingested with the same `gram.mcp.*` attributes, with `gram.mcp.server_url` = `mcp_server_resource` when present, else `mcp-tool://<mcp_server_name>`.
- R5. The shadow_mcp scanner treats a `mcp-tool://` server identity as **unresolved** (existing behaviour for name-only evidence is preserved: no definitive shadow finding on a name alone).
- R6. The inventory API reports `target_kind = "tool_namespace"` for `mcp-tool://` rows and a new per-server `sources` list (the `hook_source` values that observed the server) so the dashboard can label a row "seen via LiteLLM".
- R7. Review decisions on `tool_namespace` rows are recorded without writing enforcement (same as `stdio_command`).
- R8. The dashboard renders `tool_namespace` rows distinctly (name, "identity unresolved" badge, sources), keeps Status as "—", and allows opening the server detail page since usage exists.
- R9. Demo seed contains at least one LiteLLM-observed `tool_namespace` server with usage, so the demo org and local dev show the feature.

## Global Constraints

- Never include customer-identifying information in any file, branch, commit, or PR (repo is public). Use "the customer" / placeholders.
- Do not run `mise run gen:sdk` in a task; the integrator runs it once after all server design changes land.
- Only Task 4 (access/design) runs `mise run gen:goa-server`.
- No new ClickHouse or Postgres schema changes. If you believe one is unavoidable, stop and report instead of adding a migration.
- Follow the `golang` skill: black-box `package x_test` tests, `it ...`-style subtest names, `require`, no `//nolint` additions.
- Keep changes inside the files listed for your task. If a change elsewhere is unavoidable, make it minimal and call it out in your report.
- Do not commit. The integrator commits per task after review.
- Tests that need Postgres/ClickHouse use the package's existing `testenv` fixture; if Docker is unavailable in your environment, still write the tests, make the package compile with `go vet ./internal/<pkg>/`, and report which tests could not be executed.

---

## Shared interface (already implemented, do not modify)

`server/internal/shadowmcp/tool_namespace.go`:

```go
const ToolNamespaceScheme = "mcp-tool"
func ToolNamespaceURL(serverName string) string   // "GitHub" -> "mcp-tool://github"; "" when invalid
func IsToolNamespaceURL(value string) bool
func ToolNamespaceServer(value string) string     // "mcp-tool://github" -> "github"
```

Existing helpers you will use:

- `server/internal/toolref/toolref.go`: `IsMCPToolName(name) bool`, `MCPServerOf(name) string` (server segment of `mcp__<server>__<tool>`; `""` for non-MCP; note the `MCP:<fn>` Cursor form returns the function, not a server — treat it as **no server**).
- `server/internal/shadowmcp/inventory_url.go`: `CanonicalizeInventoryURL(raw) (InventoryURL, bool)`, `ServerSlug(canonicalURL) string`.
- `server/internal/shadowmcp/hosted.go`: `IsGramHostedMCPURL(rawURL, trustedHosts...) bool`.
- `server/internal/attr`: `MCPServerURLKey` (`gram.mcp.server_url`), `MCPMatchKey` (`gram.mcp.match`), `ToolCallSourceKey` (`gram.tool_call.source`), `HookSourceKey` (`gram.hook.source`).
- `server/internal/hooks/impl.go:335` `hashToolCallIDToTraceID(id) string` — unexported; Task 2 exports an equivalent (see Interfaces).

---

### Task 1: LiteLLM guardrail request — extract MCP inventory from `tools`

**Files:**

- Create: `server/internal/litellm/mcp_evidence.go`
- Create: `server/internal/litellm/mcp_evidence_test.go`
- Modify: `server/internal/litellm/impl.go` (the `ingestRequest` branch, around the `hookPayload` literal at ~lines 176-215; set `Data.McpInventory` and `Data.McpInventoryCollected`)
- Modify (test): `server/internal/litellm/ingest_test.go` — add cases

**Interfaces:**

- Produces:
  ```go
  // mcpInventoryFromTools derives the MCP servers an agent has configured from
  // the tool definitions LiteLLM forwarded. Name-only servers get a
  // shadowmcp.ToolNamespaceURL identity; hosted-MCP tool entries keep their URL.
  // Entries are de-duplicated by URL and sorted for stable output.
  func mcpInventoryFromTools(tools []any) []*hooksgen.HookMCPData
  ```
  Each returned `HookMCPData` sets `ServerName` (the `<server>` segment, or `server_label`, or the URL host when no label) and `URL`. Other fields nil.
- Consumes: `toolref.IsMCPToolName`, `toolref.MCPServerOf`, `shadowmcp.ToolNamespaceURL`.

Tool shapes to handle (LiteLLM forwards `tools` verbatim; entries are `map[string]any` after JSON decoding):

1. `{"type":"function","function":{"name":"mcp__github__create_issue", ...}}` → server `github` → `mcp-tool://github`.
2. `{"type":"mcp","server_label":"linear","server_url":"https://mcp.linear.app/mcp", ...}` → `URL = server_url`, `ServerName = server_label` (fallback: URL host).
3. Anything else (bare function names, built-in tools) → ignored.

- [ ] **Step 1: Write the failing tests** in `mcp_evidence_test.go` (package `litellm_test` if the package's other tests are black-box; otherwise follow the package's convention — check `ingest_test.go`'s package clause and, if it is `package litellm`, export nothing and test in-package):

```go
func TestMCPInventoryFromTools(t *testing.T) {
	t.Parallel()
	tools := []any{
		map[string]any{"type": "function", "function": map[string]any{"name": "mcp__GitHub__create_issue"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "mcp__github__list_issues"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "bash"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "MCP:search"}},
		map[string]any{"type": "mcp", "server_label": "linear", "server_url": "https://mcp.linear.app/mcp"},
		map[string]any{"type": "mcp", "server_url": "https://mcp.example.com/sse"},
		map[string]any{"type": "code_interpreter"},
		"not-an-object",
	}
	got := mcpInventoryFromTools(tools)
	require.Len(t, got, 3)
	// sorted by URL
	require.Equal(t, "https://mcp.example.com/sse", *got[0].URL)
	require.Equal(t, "mcp.example.com", *got[0].ServerName)
	require.Equal(t, "https://mcp.linear.app/mcp", *got[1].URL)
	require.Equal(t, "linear", *got[1].ServerName)
	require.Equal(t, "mcp-tool://github", *got[2].URL)
	require.Equal(t, "github", *got[2].ServerName)
}

func TestMCPInventoryFromTools_Empty(t *testing.T) {
	t.Parallel()
	require.Nil(t, mcpInventoryFromTools(nil))
	require.Nil(t, mcpInventoryFromTools([]any{map[string]any{"type": "function", "function": map[string]any{"name": "bash"}}}))
}
```

- [ ] **Step 2: Run to verify failure:** `cd server && go test ./internal/litellm/ -run TestMCPInventoryFromTools -count=1` → FAIL: undefined `mcpInventoryFromTools`.

- [ ] **Step 3: Implement** `mcp_evidence.go`:

```go
package litellm

import (
	"net/url"
	"sort"
	"strings"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

func mcpInventoryFromTools(tools []any) []*hooksgen.HookMCPData {
	byURL := map[string]*hooksgen.HookMCPData{}
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(conv.ToString(tool["type"]))) { // use the package's existing any->string helper or a local one
		case "function":
			fn, _ := tool["function"].(map[string]any)
			name := strings.TrimSpace(conv.ToString(fn["name"]))
			if !toolref.IsMCPToolName(name) || !strings.HasPrefix(name, "mcp__") {
				continue
			}
			server := toolref.MCPServerOf(name)
			identity := shadowmcp.ToolNamespaceURL(server)
			if identity == "" {
				continue
			}
			byURL[identity] = &hooksgen.HookMCPData{ServerName: conv.Ptr(strings.ToLower(server)), URL: conv.Ptr(identity)}
		case "mcp":
			serverURL := strings.TrimSpace(conv.ToString(tool["server_url"]))
			if serverURL == "" {
				continue
			}
			label := strings.TrimSpace(conv.ToString(tool["server_label"]))
			if label == "" {
				if u, err := url.Parse(serverURL); err == nil {
					label = u.Host
				}
			}
			byURL[serverURL] = &hooksgen.HookMCPData{ServerName: conv.Ptr(label), URL: conv.Ptr(serverURL)}
		}
	}
	if len(byURL) == 0 {
		return nil
	}
	out := make([]*hooksgen.HookMCPData, 0, len(byURL))
	for _, e := range byURL {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return *out[i].URL < *out[j].URL })
	return out
}
```

`exhaustruct` lint requires every `HookMCPData` field be listed explicitly (set the others to `nil`) — check `server/gen/hooks/service.go` for the struct fields. If `conv.ToString` does not exist, write a 4-line local `anyString(v any) string` that handles `string` and returns `""` otherwise.

- [ ] **Step 4: Wire into `ingestRequest`** in `impl.go`: replace `McpInventory: nil, McpInventoryCollected: nil` in the `prompt.submitted` payload with

  ```go
  McpInventory:          mcpInventory,
  McpInventoryCollected: conv.Ptr(len(mcpInventory) > 0),
  ```

  where `mcpInventory := mcpInventoryFromTools(payload.Tools)` is computed before the literal. Check `HookIngestData.McpInventoryCollected`'s type in `server/gen/hooks/service.go` (likely `*bool`).

  **Important:** `ingestRequest` returns early with `noneResult()` when `prompt == ""` (line ~150). A tools-only request with no text must still record inventory. Move the inventory computation above that early return and, when `prompt == ""` but `len(mcpInventory) > 0`, still call `IngestAuthenticatedDetailed` with an empty prompt **only if** the hooks path accepts a `prompt.submitted` with an empty prompt without writing a chat row — verify in `server/internal/hooks/ingest_hooks.go` (`case "prompt.submitted"`). If it would write an empty message, instead skip the ingest and call the inventory upsert path directly is NOT possible from this package; so in that case keep the early return and document the limitation in your report.

- [ ] **Step 5: Add an ingest test** in `ingest_test.go` following the file's existing pattern (look at how it builds an `IngestPayload` with `RequestData` and asserts on chat rows / hook effects): send a request whose `Tools` contains one `mcp__github__x` function tool and one `type: "mcp"` entry; assert (via the hooks service or ClickHouse fixture used by neighbouring tests) that `shadow_mcp_inventory_urls` gains rows `mcp-tool://github` and the hosted URL for the project. If the package has no ClickHouse fixture, assert instead through a captured hooks payload (the tests may use a fake `hooks` dependency — follow what exists). Name it `TestIngest_RecordsMCPInventoryFromTools`.

- [ ] **Step 6: Run** `go test ./internal/litellm/ -run 'TestMCPInventoryFromTools|TestIngest_RecordsMCPInventoryFromTools' -count=1` → PASS. Then `go vet ./internal/litellm/`.

- [ ] **Step 7: Report** files changed, tests run, and anything from Step 4's verification.

---

### Task 2: LiteLLM guardrail response — per-tool-call MCP provenance rows

**Files:**

- Create: `server/internal/litellm/mcp_provenance.go`
- Create: `server/internal/litellm/mcp_provenance_test.go`
- Modify: `server/internal/litellm/impl.go` (`ingestResponse`, after the successful `IngestAuthenticatedDetailed` call at ~line 349)
- Modify: `server/internal/hooks/impl.go` — add exported `HashToolCallIDToTraceID(toolCallID string) string` that `hashToolCallIDToTraceID` delegates to (one-line rename + wrapper; keep the unexported name so callers don't change).

**Interfaces:**

- Consumes: `shadowmcp.ToolNamespaceURL`, `toolref.IsMCPToolName`, `toolref.MCPServerOf`, `hooks.HashToolCallIDToTraceID`, the litellm `Service`'s telemetry logger (see how `trace_processor.go` / `otlp_dispatch.go` write rows — reuse the same logger and row type).
- Produces:
  ```go
  // mcpToolCallProvenance is one LiteLLM-observed MCP tool call to record.
  type mcpToolCallProvenance struct {
      ToolCallID string // LiteLLM/OpenAI tool_call id
      ToolName   string // full namespaced name
      Server     string // <server> segment, lower-cased
      Identity   string // shadowmcp.ToolNamespaceURL(Server)
  }
  func mcpToolCallProvenanceFrom(toolCalls []any) []mcpToolCallProvenance
  func (s *Service) recordMCPToolCallProvenance(ctx context.Context, in mcpProvenanceInput) // best-effort, logs warnings, never returns an error to the caller
  ```
  `mcpProvenanceInput` carries: project ID, org ID, user email (`payload.RequestData.UserAPIKeyUserEmail`), user id if the hooks path resolved one (may be empty), session ID, litellm call/trace id, model, and the `[]mcpToolCallProvenance`.

Tool-call shape (OpenAI): `{"id":"call_abc","type":"function","function":{"name":"mcp__github__create_issue","arguments":"{...}"}}`. Also accept the chunk form where `function` may be missing `arguments`. Skip entries without `id` or without an MCP-namespaced `mcp__` name.

Row attributes to set (mirror how `otlp.go`/`trace_processor.go` build `telemetry_logs` rows — use the same `telemetry.Log`/bulk API and the same identity attributes it uses for `user_email`):

- `trace_id` = `hooks.HashToolCallIDToTraceID(ToolCallID)`
- `gram_urn` = `"litellm:guardrail:tool_call"` (must **not** start with `urn:uuid:` or `trace_summaries_mv` drops it)
- `attributes.gram.mcp.server_url` = Identity, `attributes.gram.mcp.match` = Identity, `attributes.gram.tool_call.source` = Server
- `attributes.gram.hook.source` = `"litellm"` (this materializes into `hook_source`)
- `attributes.gen_ai.tool.name` = ToolName (check the attr key constant used by hooks for tool name; use the same)
- user email attribute as the OTLP path sets it (`attr.LiteLLMUserEmailKey`) **and** whatever column/attribute `trace_summaries_mv`'s `any(user_email)` reads from — inspect `server/clickhouse/schema.sql` `telemetry_logs` for the `user_email` column definition (materialized from which attribute?) and set that attribute.
- `event_source`/`tool_name` columns: inspect `telemetry_logs` materialized columns and set the source attributes so `tool_name` is the ToolName.

- [ ] **Step 1: Failing unit test** for `mcpToolCallProvenanceFrom`:

```go
func TestMCPToolCallProvenanceFrom(t *testing.T) {
	t.Parallel()
	calls := []any{
		map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "mcp__GitHub__create_issue", "arguments": "{}"}},
		map[string]any{"id": "call_2", "type": "function", "function": map[string]any{"name": "bash"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "mcp__github__x"}}, // no id
		map[string]any{"id": "call_3", "type": "function", "function": map[string]any{"name": "MCP:search"}},
	}
	got := mcpToolCallProvenanceFrom(calls)
	require.Equal(t, []mcpToolCallProvenance{{
		ToolCallID: "call_1", ToolName: "mcp__GitHub__create_issue", Server: "github", Identity: "mcp-tool://github",
	}}, got)
}
```

- [ ] **Step 2:** run → FAIL undefined. **Step 3:** implement in `mcp_provenance.go`. **Step 4:** run → PASS.

- [ ] **Step 5: Integration test** `TestIngestResponse_RecordsMCPToolCallProvenance` in `mcp_provenance_test.go` using the package's ClickHouse fixture (see `traces_test.go` for how OTLP tests assert `telemetry_logs` rows — reuse that helper): ingest a `response` payload with one MCP tool call; flush async inserts (`testenv.FlushClickHouseAsyncInserts` if the package uses async inserts); query `telemetry_logs` for `trace_id = HashToolCallIDToTraceID("call_1")` and assert `attributes.gram.mcp.server_url = 'mcp-tool://github'`, `hook_source = 'litellm'`, `tool_source = 'github'`, and the user email. Then query `trace_summaries` for the same trace and assert `mcp_server_url`.

- [ ] **Step 6: Wire** in `ingestResponse` after the ingest call succeeds:

  ```go
  if prov := mcpToolCallProvenanceFrom(payload.ToolCalls); len(prov) > 0 {
      s.recordMCPToolCallProvenance(ctx, mcpProvenanceInput{ /* ... */ ToolCalls: prov })
  }
  ```

  Also run it when the assistant turn was **dropped as a duplicate** of a native hook stream? No — if a native device stream covers the session, the device already records provenance; recording twice would double usage. `IngestAuthenticatedDetailed` returns an outcome; check whether it exposes "persisted" — if it does, gate on it; if not, record unconditionally and note it in your report.

- [ ] **Step 7:** `go test ./internal/litellm/ -run 'Provenance' -count=1` → PASS; `go vet ./internal/litellm/ ./internal/hooks/`. Report.

---

### Task 3: LiteLLM OTLP — MCP gateway span metadata → `gram.mcp.*` attributes

**Files:**

- Create: `server/internal/litellm/otlp_mcp.go`
- Create: `server/internal/litellm/otlp_mcp_test.go`
- Modify: `server/internal/litellm/otlp.go` (attribute mapping) and/or `server/internal/litellm/trace_processor.go` (wherever span attributes are turned into a row's attribute map — find the function that applies `metricAttributeAllowlist` and add a post-step)

**Interfaces:**

- Produces:
  ```go
  // mcpToolCallMetadata is the subset of LiteLLM's StandardLoggingMCPToolCall
  // that identifies the upstream server.
  type mcpToolCallMetadata struct {
      Name               string `json:"name"`
      NamespacedToolName string `json:"namespaced_tool_name"`
      MCPServerName      string `json:"mcp_server_name"`
      MCPServerResource  string `json:"mcp_server_resource"` // origin: scheme+host+port
  }
  func parseMCPToolCallMetadata(raw string) (mcpToolCallMetadata, bool)
  // mcpSpanAttributes returns the gram.mcp.* / gram.tool_call.source attributes to
  // add for a span, or nil when the span is not an MCP tool call.
  func mcpSpanAttributes(md mcpToolCallMetadata) map[attribute.Key]string
  ```
- Rule: `server_url = MCPServerResource` when non-empty and parses with a host; else `shadowmcp.ToolNamespaceURL(MCPServerName)`; if both empty → nil (no attributes). `gram.mcp.match = server_url`; `gram.tool_call.source = MCPServerName` (fallback: `ToolNamespaceServer(server_url)` or the URL host). Also set `gram.hook.source = "litellm"` if the OTLP path does not already set it (check what `trace_processor.go` sets; do not duplicate).

LiteLLM emits the attribute as `metadata.mcp_tool_call_metadata` (JSON string; `safe_set_attribute` stringifies dicts). Some exporters may also emit `litellm.metadata.mcp_tool_call_metadata` — accept both keys, same as the existing allowlist does for `user_api_key_user_email`.

- [ ] **Step 1: Failing tests:**

```go
func TestParseMCPToolCallMetadata(t *testing.T) {
	t.Parallel()
	md, ok := parseMCPToolCallMetadata(`{"name":"create_issue","namespaced_tool_name":"github/create_issue","mcp_server_name":"github","mcp_server_resource":"https://api.githubcopilot.com","arguments":{"title":"x"}}`)
	require.True(t, ok)
	require.Equal(t, "github", md.MCPServerName)
	require.Equal(t, "https://api.githubcopilot.com", md.MCPServerResource)
	_, ok = parseMCPToolCallMetadata("not json")
	require.False(t, ok)
	_, ok = parseMCPToolCallMetadata("")
	require.False(t, ok)
}

func TestMCPSpanAttributes(t *testing.T) {
	t.Parallel()
	got := mcpSpanAttributes(mcpToolCallMetadata{MCPServerName: "github", MCPServerResource: "https://api.githubcopilot.com"})
	require.Equal(t, "https://api.githubcopilot.com", got[attr.MCPServerURLKey])
	require.Equal(t, "https://api.githubcopilot.com", got[attr.MCPMatchKey])
	require.Equal(t, "github", got[attr.ToolCallSourceKey])

	got = mcpSpanAttributes(mcpToolCallMetadata{MCPServerName: "Internal Docs"})
	require.Equal(t, "", got[attr.MCPServerURLKey]) // name has a space -> no synthetic identity
	got = mcpSpanAttributes(mcpToolCallMetadata{MCPServerName: "internal_docs"})
	require.Equal(t, "mcp-tool://internal_docs", got[attr.MCPServerURLKey])

	require.Nil(t, mcpSpanAttributes(mcpToolCallMetadata{}))
}
```

- [ ] **Step 2:** run → FAIL. **Step 3:** implement `otlp_mcp.go`. **Step 4:** run → PASS.

- [ ] **Step 5: End-to-end trace test** in `otlp_mcp_test.go`, modelled on an existing test in `traces_test.go` that posts an OTLP JSON export and asserts `telemetry_logs` rows: post one span with attributes `gen_ai.request.model = "MCP: create_issue"`, `metadata.user_api_key_user_email = "dev@example.com"`, and `metadata.mcp_tool_call_metadata = <json above>`; assert the stored row has `attributes.gram.mcp.server_url = 'https://api.githubcopilot.com'` and `tool_source = 'github'`, and that `trace_summaries` has `mcp_server_url` for that trace.

- [ ] **Step 6:** `go test ./internal/litellm/ -run 'MCP' -count=1`; `go vet ./internal/litellm/`. Report.

---

### Task 4: Access service + API design — `tool_namespace` rows, `sources`, observe-only decisions; scanner unresolved rule

**Files:**

- Modify: `server/design/access/design.go` — `ShadowMCPInventoryServerModel` (~line 849): add `"tool_namespace"` to the `target_kind` `Enum`, update its `Description`; add `Attribute("sources", ArrayOf(String), "Hook sources (claude-code, cursor, litellm, ...) that observed this server. Empty when the server is known only from a review request.")`.
- Modify: `server/internal/access/shadow_mcp_inventory.go` — constants (~line 59), `ListShadowMCPInventory` (~212), `buildShadowMCPInventoryServer` (~1469), `buildShadowMCPRequestOnlyServer` (~280), the request-decision path that skips enforcement for non-`server_url` kinds (~314-318), `GetShadowMCPInventoryServer` (~388) and the by-request variant (~437).
- Modify: `server/internal/telemetry/repo/queries.sql.go` — `listShadowMCPInventoryTraceUsage` (~1468-1537): add `groupUniqArray(hook_source) AS sources` (aggregate over the per-trace `max(hook_source)`), scan into `ShadowMCPInventoryUsageRow.Sources []string`.
- Modify: `server/internal/scanners/shadowmcpscan/scanner.go` — `resolvedServerIdentity` (~line 419).
- Test: `server/internal/access/list_shadow_mcp_inventory_test.go` (or the existing inventory test file — find it with `ls server/internal/access/*shadow*test.go`), `server/internal/scanners/shadowmcpscan/scanner_test.go`, `server/internal/telemetry/repo/*_test.go` neighbour of the usage query.
- Run: `mise run gen:goa-server` after the design edit (regenerates `server/gen/**`).

**Interfaces:**

- Produces: `const shadowMCPTargetKindToolNamespace = "tool_namespace"`; `func shadowMCPTargetKindForURL(canonicalURL string) string` returning `tool_namespace` when `shadowmcp.IsToolNamespaceURL(canonicalURL)`, else `server_url`; `gen.ShadowMCPInventoryServer.Sources []string` populated from usage.
- Consumes: `shadowmcp.IsToolNamespaceURL`, `shadowmcp.ToolNamespaceServer`.

- [ ] **Step 1 (scanner): failing test** in `scanner_test.go` next to the existing `resolvedServerIdentity` tests (search for `resolvedServerIdentity` or `it reports unresolved`):
  ```go
  t.Run("it treats a tool-namespace server_url as unresolved", func(t *testing.T) {
      _, ok := resolvedServerIdentity(telemetryrepo.MCPProvenance{ServerURL: "mcp-tool://github", Match: "mcp-tool://github"}, "mcp__github__")
      require.False(t, ok)
  })
  ```
  (If the tests are black-box and the function unexported, drive it through `Scanner.Scan` with a fixture provenance row the way neighbouring tests do, asserting the finding's resolution metric/`Match` behaves as for the bare-prefix case.)
- [ ] **Step 2:** run → FAIL. **Step 3:** in `resolvedServerIdentity`, before returning `serverURL`, add: `if shadowmcp.IsToolNamespaceURL(serverURL) { return "", false }` and extend the doc comment: a tool-namespace identity carries the same information as the bare tool prefix, so it is unresolved for the same reason. Also apply the same check to `match` after the prefix comparison. **Step 4:** run → PASS.

- [ ] **Step 5 (usage query): failing test** for `Sources` in the telemetry repo test file that covers `ListShadowMCPInventoryUsage`: insert two `telemetry_logs` rows for the same `gram.mcp.server_url` with `gram.hook.source` `claude-code` and `litellm`; assert the usage row's `Sources` equals `[]string{"claude-code","litellm"}` (sort before comparing). **Step 6:** implement: in the trace-level subquery `max(hook_source) AS hook_source` already exists; in the outer aggregate add `arraySort(groupUniqArray(hook_source)) AS sources` and filter empty strings with `arrayFilter(x -> x != '', ...)`. Add `Sources []string` to the row struct and the scan. Note the `ILLEGAL_AGGREGATION` gotcha from the clickhouse skill: alias inner aggregates to non-colliding names if the outer query aggregates over them. **Step 7:** run → PASS.

- [ ] **Step 8 (design):** edit `design.go` as listed; run `mise run gen:goa-server`; `go build ./...` will fail with `exhaustruct` hints where `gen.ShadowMCPInventoryServer` literals miss `Sources` — fix them in `buildShadowMCPInventoryServer` (`Sources: usage.Sources` — use an empty non-nil slice when nil so JSON emits `[]`) and `buildShadowMCPRequestOnlyServer` (`Sources: []string{}`).

- [ ] **Step 9 (target kind): failing test** in the access inventory test: seed a `shadow_mcp_inventory_urls` row with `canonical_server_url = 'mcp-tool://github'`, `url_host = 'github'`, `server_name = 'github'` plus usage rows with `hook_source = 'litellm'`; call `ListShadowMCPInventory`; assert the row has `TargetKind == "tool_namespace"`, `Sources == []string{"litellm"}`, `ServerSlug` non-empty, and `AccessSummary.State` equal to whatever stdio rows report today (find the stdio expectation in the existing tests and mirror it). **Step 10:** implement `shadowMCPTargetKindForURL` and use it at every `buildShadowMCPInventoryServer(..., shadowMCPTargetKindServerURL)` call site that passes a row/usage URL (lines ~212, ~388, ~437, ~656). In the decision path that checks `request.TargetKind != shadowMCPTargetKindServerURL` to skip enforcement writes (~314-318), also skip when `shadowmcp.IsToolNamespaceURL(targetKey)`. **Step 11:** run the access tests → PASS.

- [ ] **Step 12:** `mise run lint:server` (scoped: `cd server && golangci-lint` is NOT to be invoked directly — use the mise task) and `go test ./internal/access/... ./internal/scanners/shadowmcpscan/... ./internal/telemetry/repo/... -count=1`. Report, including the list of `server/gen/**` files regenerated.

---

### Task 5: Dashboard — render `tool_namespace` rows and sources (runs after Task 4 + `mise run gen:sdk`)

**Files:**

- Modify: `client/dashboard/src/components/shadow-mcp/ShadowMCPInventoryCells.tsx` (`ShadowMCPInventoryServerCell`, ~line 18-60)
- Modify: `client/dashboard/src/components/shadow-mcp/ShadowMCPInventoryTable.tsx` (~224 `isStdio`, ~271 Status column, ~401 test fixture kind)
- Modify: `client/dashboard/src/components/shadow-mcp/ShadowMCPInventoryTable.test.tsx`, `ShadowMCPInventoryCells` tests if present
- Modify: `client/dashboard/src/pages/shadow-mcp/ShadowMCPServerDetail.tsx` — header shows "Identity unresolved · seen via <sources>" for `tool_namespace`
- Possibly: `client/dashboard/src/components/mcp-approvals/DecideAccessSheet.tsx` if it switches on `targetKind`

**Interfaces:**

- Consumes: `ShadowMCPInventoryServer.targetKind: "server_url" | "stdio_command" | "tool_namespace"`, `ShadowMCPInventoryServer.sources?: string[]` from the regenerated SDK in `client/dashboard/src/sdk/`.

Behaviour:

- Server cell for `tool_namespace`: primary text = `server.serverName` (fallback `urlHost`); secondary = the raw tool namespace, e.g. `mcp__github__*`; a warning badge "Identity unresolved" with a tooltip "Seen only by the LLM proxy as a tool namespace. Configure the LiteLLM MCP gateway to resolve the server URL." Reuse the stdio cell's badge styling.
- Sources: small muted text or chips listing `server.sources` (map `litellm` → "LiteLLM", others via the existing agent-source label helper if one exists — search for `sourceAliases`/`agentSourceLabel` in the dashboard).
- Status column: "—" for `tool_namespace` (same as stdio).
- Row click: opens the server detail page (unlike stdio), because usage and users exist.
- Follow the `frontend` skill; run `aube run -F dashboard type-check` and the component tests (`aube run -F dashboard test -- ShadowMCPInventory`).

- [ ] **Step 1:** add failing tests in `ShadowMCPInventoryTable.test.tsx`: a `tool_namespace` fixture renders "Identity unresolved", shows "LiteLLM" in sources, Status renders "—", and clicking the row calls `onOpenServer`. **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS. **Step 5:** type-check. Report.

---

### Task 6: Demo seed + changesets (runs after Tasks 1-4)

**Files:**

- Modify: `server/internal/demoseed/clickhouse.sql` (and `postgres.sql` only if a review request is desired) — follow the `gram-demo-seed` skill (`.agents/skills/gram-demo-seed/SKILL.md`).
- Create: `.changeset/litellm-shadow-mcp-inventory.md`

- [ ] **Step 1:** Activate the `gram-demo-seed` skill and add: one `shadow_mcp_inventory_urls` row `mcp-tool://github` (server_name `github`) and 3-5 `telemetry_logs` rows for the demo project with `gram.hook.source = 'litellm'`, `gram.mcp.server_url = 'mcp-tool://github'`, `gram.tool_call.source = 'github'`, distinct `trace_id`s, demo user emails, `gram_urn = 'litellm:guardrail:tool_call'`, within the last 7 days relative to seed time (follow how existing seed rows express time). Also one URL-grade LiteLLM-observed server via OTLP shape (`gram.hook.source='litellm'`, real `https://` URL) so both kinds appear.
- [ ] **Step 2:** run the seed safety test named in the skill (`TestDemoSeedSafety`) and `mise run seed` if the local stack is available.
- [ ] **Step 3:** changeset:
  ```md
  ---
  "server": minor
  "dashboard": patch
  ---

  Shadow MCP inventory now includes MCP servers observed through a LiteLLM proxy. Servers LiteLLM knows only by tool namespace appear as `tool_namespace` rows (observe-only, identity unresolved); servers reached through the LiteLLM MCP gateway or declared as hosted-MCP tools appear as normal URL rows. Inventory rows report the hook sources that observed them.
  ```

---

## Integration (integrator, after all tasks)

1. `mise run gen:sdk` → commit SDK; then Task 5.
2. `mise run build:server && mise run lint:server && mise run test:server ./internal/litellm/... ./internal/access/... ./internal/scanners/... ./internal/telemetry/...`
3. `aube run -F dashboard type-check`, dashboard tests.
4. Manual check with `mise run playwright` on the Shadow MCP page against seeded data; capture a screenshot for the PR (`pr-demo-gif`).
5. PR via the `pull-request` skill; title `feat: LiteLLM shadow MCP inventory support`; body must not name the customer.

## Self-review

- R1 → Task 1. R2 → Task 1. R3 → Task 2. R4 → Task 3. R5 → Task 4 steps 1-4. R6 → Task 4 steps 5-11. R7 → Task 4 step 10. R8 → Task 5. R9 → Task 6.
- Open risk carried into review: Task 2 double-counting when a device hook stream covers the same session; Task 1 tools-only requests with an empty prompt. Both are called out in their steps with a decision rule and a reporting requirement.
