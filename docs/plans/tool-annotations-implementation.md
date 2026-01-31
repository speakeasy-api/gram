# MCP Tool Annotations Implementation Plan

## Overview

This document outlines the plan to implement MCP tool annotations support in Gram. Tool annotations provide hints about tool behavior that clients can use to improve UX, safety, and decision-making.

## MCP Tool Annotations Specification

Per the [MCP specification](https://modelcontextprotocol.io/specification/2025-11-25), the `annotations` object on a tool contains:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `title` | string | — | Human-readable display name for the tool |
| `readOnlyHint` | boolean | `false` | Tool performs no environment modifications |
| `destructiveHint` | boolean | `true` | Tool may perform destructive updates (only meaningful when `readOnlyHint=false`) |
| `idempotentHint` | boolean | `false` | Repeated calls with same args have no additional effect (only meaningful when `readOnlyHint=false`) |
| `openWorldHint` | boolean | `true` | Tool interacts with external entities vs. closed domain |

**Important**: These are *hints*, not guarantees. Clients should not rely solely on annotations for security decisions with untrusted servers.

## Current State Analysis

### What Gram Has Today

1. **Registry Client** (`server/internal/externalmcp/registryclient.go:150`):
   - Stores annotations as `map[string]any` when fetching from MCP registries
   - Passes through to `ExternalMCPTool` type

2. **Catalog UI** (`client/dashboard/src/pages/catalog/CatalogDetail.tsx:466`):
   - Shows "Read-only" badge when `readOnlyHint=true`
   - Partial support for `title`, `readOnlyHint`, `destructiveHint` in type definition

3. **MCP Server Response** (`server/internal/mcp/rpc_tools_list.go`):
   - `toolListEntry` struct has `Meta` field but no `Annotations` field
   - Does not currently expose annotations in `tools/list` response

4. **External MCP Client** (`server/internal/externalmcp/mcpclient.go:178`):
   - `Tool` struct captures `Name`, `Description`, `Schema` only
   - Does not capture `Annotations` from external servers

### Gaps to Address

1. **MCP Server**: Gram's MCP servers don't expose tool annotations
2. **External MCP Passthrough**: Annotations from external MCP servers are lost when proxying
3. **UI Coverage**: Only `readOnlyHint` shown; missing `destructiveHint`, `idempotentHint`, `openWorldHint`
4. **Confirmation Flow**: Tool approval doesn't consider annotation hints
5. **Storage**: No way to set annotations on HTTP/Function tools in Gram

---

## Implementation Plan

### Phase 1: Data Model & Schema Updates

#### 1.1 Add ToolAnnotations Type (Goa Design)

**File**: `server/design/shared/tools.go`

```go
var ToolAnnotations = Type("ToolAnnotations", func() {
    Meta("struct:pkg:path", "types")
    Description("MCP tool annotations providing hints about tool behavior")

    Attribute("title", String, "Human-readable display name for the tool")
    Attribute("read_only_hint", Boolean, func() {
        Description("If true, the tool does not modify its environment")
        Default(false)
    })
    Attribute("destructive_hint", Boolean, func() {
        Description("If true, the tool may perform destructive updates")
        Default(true)
    })
    Attribute("idempotent_hint", Boolean, func() {
        Description("If true, repeated calls with same args have no additional effect")
        Default(false)
    })
    Attribute("open_world_hint", Boolean, func() {
        Description("If true, tool interacts with external entities")
        Default(true)
    })
})
```

#### 1.2 Add Annotations to Tool Types

Update `BaseToolAttributes` in `server/design/shared/tools.go`:

```go
Attribute("annotations", ToolAnnotations, "MCP tool annotations")
```

#### 1.3 Database Schema Update

**File**: `server/database/schema.sql`

Add `annotations` JSONB column to `tools` table:

```sql
alter table tools add column annotations jsonb;
```

Run `mise db:diff tools-add-annotations-column` to generate migration.

---

### Phase 2: MCP Server Annotations Support

#### 2.1 Update Tool List Entry

**File**: `server/internal/mcp/rpc_tools_list.go`

```go
type toolListEntry struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema,omitempty,omitzero"`
    Annotations *toolAnnotations `json:"annotations,omitempty"`
    Meta        map[string]any  `json:"_meta,omitempty"`
}

type toolAnnotations struct {
    Title          string `json:"title,omitempty"`
    ReadOnlyHint   *bool  `json:"readOnlyHint,omitempty"`
    DestructiveHint *bool `json:"destructiveHint,omitempty"`
    IdempotentHint *bool  `json:"idempotentHint,omitempty"`
    OpenWorldHint  *bool  `json:"openWorldHint,omitempty"`
}
```

#### 2.2 Populate Annotations from Tool Definitions

Update `toolToListEntry()` and `buildToolListEntries()` to include annotations from:
- HTTP tools (derive from HTTP method: GET = readOnly, DELETE = destructive)
- Function tools (from stored annotations)
- External MCP tools (passthrough from source)

#### 2.3 HTTP Method Inference

For HTTP tools without explicit annotations, infer defaults:

| HTTP Method | readOnlyHint | destructiveHint | idempotentHint |
|-------------|--------------|-----------------|----------------|
| GET | true | false | true |
| HEAD | true | false | true |
| OPTIONS | true | false | true |
| POST | false | false | false |
| PUT | false | false | true |
| PATCH | false | false | false |
| DELETE | false | true | true |

---

### Phase 3: External MCP Passthrough

#### 3.1 Update External MCP Client

**File**: `server/internal/externalmcp/mcpclient.go`

```go
type Tool struct {
    Name        string
    Description string
    Schema      json.RawMessage
    Annotations map[string]any  // Add this field
}
```

Update `ListTools()` to capture annotations:

```go
tools = append(tools, Tool{
    Name:        tool.Name,
    Description: tool.Description,
    Schema:      schema,
    Annotations: tool.Annotations,  // Add this
})
```

#### 3.2 Forward Annotations in Proxy

**File**: `server/internal/mcp/rpc_tools_list.go`

Update the external tool loop in `buildToolListEntries()`:

```go
for _, extTool := range proxyTools {
    tools = append(tools, &toolListEntry{
        Name:        extTool.Name,
        Description: extTool.Description,
        InputSchema: extTool.Schema,
        Annotations: convertAnnotations(extTool.Annotations),
        Meta:        nil,
    })
}
```

---

### Phase 4: UI Enhancements

#### 4.1 Tool Card Badges

**File**: `client/dashboard/src/pages/catalog/CatalogDetail.tsx`

Enhance the `Tool` type and `ToolCard` component:

```tsx
type Tool = {
  name: string;
  description?: string;
  annotations?: {
    title?: string;
    readOnlyHint?: boolean;
    destructiveHint?: boolean;
    idempotentHint?: boolean;
    openWorldHint?: boolean;
  };
};

function ToolCard({ tool }: { tool: Tool }) {
  return (
    <div className="...">
      <Stack direction="horizontal" gap={2} align="center">
        <Type className="font-mono text-sm font-medium">
          {tool.annotations?.title || tool.name}
        </Type>
        {tool.annotations?.readOnlyHint && (
          <Badge variant="neutral">Read-only</Badge>
        )}
        {tool.annotations?.destructiveHint && !tool.annotations?.readOnlyHint && (
          <Badge variant="warning">Destructive</Badge>
        )}
        {tool.annotations?.idempotentHint && (
          <Badge variant="info">Idempotent</Badge>
        )}
      </Stack>
      {/* ... */}
    </div>
  );
}
```

#### 4.2 Tool Execution UI Updates

**File**: `elements/src/components/ui/tool-ui.tsx`

Add annotation props and visual indicators:

```tsx
interface ToolUIProps {
  name: string;
  // ... existing props
  annotations?: {
    title?: string;
    readOnlyHint?: boolean;
    destructiveHint?: boolean;
    idempotentHint?: boolean;
    openWorldHint?: boolean;
  };
}
```

Display options:
- Use `annotations.title` as display name when available
- Show warning icon for destructive tools
- Show lock icon for read-only tools
- Color-code status indicator based on destructiveness

#### 4.3 Approval Flow Enhancement

For tools awaiting approval, surface annotation hints:

```tsx
{isApprovalPending && (
  <div className="...">
    {annotations?.destructiveHint && (
      <div className="text-warning text-sm flex items-center gap-1">
        <AlertTriangle className="w-4 h-4" />
        This tool may make destructive changes
      </div>
    )}
    {annotations?.readOnlyHint && (
      <div className="text-info text-sm flex items-center gap-1">
        <Eye className="w-4 h-4" />
        This tool only reads data
      </div>
    )}
    {/* ... existing approval buttons */}
  </div>
)}
```

---

### Phase 5: Admin Configuration

#### 5.1 Tool Editor UI

Allow users to set annotations when configuring tools in the dashboard:

- Add annotations section to tool edit forms
- Provide toggles for each hint with explanatory tooltips
- Show inferred defaults for HTTP tools (with option to override)

#### 5.2 API Endpoints

Update tool creation/update endpoints to accept annotations:

**File**: `server/design/tools/design.go`

```go
Payload(func() {
    // ... existing fields
    Attribute("annotations", ToolAnnotations, "Tool behavior annotations")
})
```

---

## UI Design Options

### Option A: Badge-Based Indicators

Simple badges next to tool names:
- `Read-only` (green/neutral)
- `Destructive` (red/warning)
- `Idempotent` (blue/info)

**Pros**: Familiar pattern, compact, scannable
**Cons**: Can get crowded with multiple badges

### Option B: Icon Indicators

Small icons with tooltips:
- Eye icon for read-only
- Trash/warning icon for destructive
- Refresh icon for idempotent
- Globe icon for open-world

**Pros**: More compact, internationally understandable
**Cons**: Requires tooltip for clarity

### Option C: Category Grouping

Group tools by behavior type in listings:
- "Safe Operations" (read-only)
- "Modifying Operations" (idempotent writes)
- "Destructive Operations" (destructive)

**Pros**: Clear risk stratification
**Cons**: Fragments tool list, may not fit all views

### Recommendation

Use **Option A** (badges) for catalog/listing views combined with **Option B** (icons) in the tool execution UI where space is limited. Add a tooltip on hover showing all annotations.

---

## Confirmation Flow Recommendations

### Current Flow

Tools with `confirm: "always"` or `confirm: "first"` trigger approval dialog.

### Enhanced Flow with Annotations

1. **Auto-approve read-only tools**: If `readOnlyHint=true` and user has opted into this, skip confirmation
2. **Warn on destructive**: If `destructiveHint=true`, show explicit warning in approval dialog
3. **Trust idempotent retries**: If `idempotentHint=true` and same args, allow auto-retry without re-confirmation

### Configuration Options

Add user preferences:
- "Auto-approve read-only tools" toggle
- "Always confirm destructive tools" toggle (default on)
- "Show annotation warnings" toggle

---

## Migration Strategy

1. **Phase 1-2**: Backend changes, no breaking changes to API
2. **Phase 3**: External MCP passthrough, backwards compatible
3. **Phase 4**: UI updates, progressive enhancement
4. **Phase 5**: Admin UI, optional feature

Each phase can be shipped independently. Start with Phase 1-2 to establish the data model, then parallelize Phase 3 and 4.

---

## Testing Plan

### Unit Tests

- Annotation serialization/deserialization
- HTTP method inference logic
- Annotation merging (explicit overrides inferred)

### Integration Tests

- MCP `tools/list` returns annotations
- External MCP tool annotations passthrough
- Tool creation with annotations via API

### E2E Tests

- Catalog displays annotation badges
- Tool execution shows annotation warnings
- Approval flow respects annotation hints

---

## Open Questions

1. Should we expose `openWorldHint` in UI? (Less actionable than others)
2. Should annotations affect default `confirm` mode for new tools?
3. How to handle annotation conflicts when proxy tool overrides source annotations?
4. Should we allow per-environment annotation overrides?

---

## References

- [MCP Specification - Tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)
- [MCP Tool Annotations Blog Post](https://blog.marcnuri.com/mcp-tool-annotations-introduction)
- [Spring AI MCP Annotations](https://docs.spring.io/spring-ai/reference/api/mcp/mcp-annotations-server.html)
