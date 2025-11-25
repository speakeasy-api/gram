# External Links

- [MCP Registry GitHub Repository](https://github.com/modelcontextprotocol/registry)
- [Official MCP Registry Documentation](https://registry.modelcontextprotocol.io/docs)

# Overview

*Replace this section with an overview of the problem space and the impact it is having on ourselves or our customers and a high level overview of the approach to take to solve it. Try to sell the problem to the reader and set the stage for solving it.*

# Goals

*Replace this section with a high level overview of the overall goal of this RFC followed by a list of goals and stretch goals:*

- A
- list
- of
- goals

# TLDR / Key Decisions

*Fill this in last after you have finished the "Proposal".  This should be a consolidated summary of the pitch.*

- Registry results will not be cached for v0 (Linear ticket to be created for future caching implementation)
- System designed to support multiple registries per organization despite v0 expecting only one (prepares for future "Gram Catalogs" feature)
- External server validation happens asynchronously to prevent blocking deployment operations on external dependencies
- **MCP resources are out of scope for v0** - only tools will be supported initially

# Proposal

*The proposal section should go into detail on how the problem will be solved. It should try to balance brevity with enough detail to get the point across. This is a living document and it could start with just high level bullet points that get broken down further and further as feedback and questions come in. Diagrams, charts, code examples all speak louder than words so use them where possible.*

## 1. Listing Available Servers from Registries

This section covers how users discover and browse external MCP servers from configured registries.

### System Diagram

```mermaid
sequenceDiagram
    participant Client as Gram Dashboard
    participant Server as Gram Server
    participant DB as PostgreSQL
    participant Registry as External Registry<br/>(e.g., MCP Registry)

    Client->>Server: ListExternalMCPCatalog(org_id)
    Server->>DB: SELECT registries<br/>WHERE org IN registry_organizations
    DB-->>Server: Available registries

    loop For each registry
        Server->>Registry: GET /v1/servers (or equivalent)
        Registry-->>Server: List of external MCP servers
    end

    Server-->>Client: Aggregated list of external servers<br/>(name, description, metadata)
```

**Database Schema:**

```sql
-- Stores external MCP registries
CREATE TABLE IF NOT EXISTS registries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    url TEXT NOT NULL,  -- API endpoint for registry
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW() ON UPDATE NOW(),
    deleted_at TIMESTAMPTZ,
    deleted BOOLEAN NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED
);

-- Controls which organizations can access which registries
CREATE TABLE IF NOT EXISTS registry_organizations (
    registry_id UUID NOT NULL REFERENCES registries(id) ON DELETE SET NULL,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW() ON UPDATE NOW(),
    deleted_at TIMESTAMPTZ,
    deleted BOOLEAN NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
    PRIMARY KEY (registry_id, organization_id)
);
```

### Implementation

**New Package:** `server/internal/external_mcp/`

This package will handle all external MCP registry interactions.

**New RPC Endpoint:** `ListExternalMCPCatalog`

Located in Goa design files, this endpoint:
- Takes organization ID as required parameter
- Takes optional registry ID parameter for pagination
- Returns list of available external MCP servers from all accessible registries
- Proxies requests to external registries in real-time (no caching for v0)

**Pagination Strategy:**
- Without `registryId`: Returns first 100 results across all registries, rejects pagination parameters
- With `registryId`: Enables pagination for that specific registry
- V0 assumes small collections; pagination primarily for future-proofing

**Package Structure:**

```
server/internal/external_mcp/
├── service.go          # Service interface and implementation
├── registry_client.go  # HTTP client for external registry APIs
├── queries.sql         # SQLc queries for registries tables
└── impl.go            # RPC endpoint implementations
```

**Flow:**
1. Client calls `ListExternalMCPCatalog(org_id, registry_id?, cursor?)`
2. Server queries `registry_organizations` to find accessible registries
3. If `registry_id` provided:
   - Validate pagination parameters allowed
   - Proxy to that specific registry with cursor
4. If no `registry_id`:
   - Reject if cursor/pagination parameters provided
   - Query all accessible registries (first 100 results total)
5. Aggregate results and return to client
6. Client displays in "Import External MCP" dialog

**Error Handling:**
- If registry is unavailable, log error but continue with other registries
- Return partial results if some registries fail
- Include registry source in response so UI can indicate which servers are from which registry

**Response Type Design:**

```go
// ListExternalMCPCatalogResponse is returned by the ListExternalMCPCatalog RPC
type ListExternalMCPCatalogResponse struct {
    Servers    []ExternalMCPServer `json:"servers"`
    NextCursor *string             `json:"nextCursor,omitempty"` // For pagination
}

// ExternalMCPServer represents an external MCP server from a registry
type ExternalMCPServer struct {
    Name        string    `json:"name"`        // Reverse-DNS identifier (e.g., "ai.exa/exa")
    Version     string    `json:"version"`     // Semantic version
    Description string    `json:"description"` // Human-readable description
    RegistryID  string    `json:"registryId"`  // Which registry this came from
    Status      string    `json:"status"`      // "active", "deprecated", "deleted"
    UpdatedAt   time.Time `json:"updatedAt"`   // When server was last updated
    Title       *string   `json:"title,omitempty"` // Friendly display name
    IconURL     *string   `json:"iconUrl,omitempty"` // First icon src (if available)
}
```

**Mapping from MCP Registry Response:**
- `Name` ← `server.name`
- `Version` ← `server.version`
- `Description` ← `server.description`
- `Status` ← `_meta.io.modelcontextprotocol.registry/official.status`
- `UpdatedAt` ← `_meta.io.modelcontextprotocol.registry/official.updatedAt`
- `Title` ← `server.title`
- `IconURL` ← `server.icons[0].src` (first icon if present)

**Note:** Only servers with `remotes[]` are supported; servers with `packages[]` are filtered during source creation.

## 2. Creating Sources from External Servers

This section covers how external MCP servers are added to deployments and how those sources are processed.

### Goa Design Changes

**Extend `CreateDeploymentForm` and `EvolveDeploymentForm`:**

```go
// In CreateDeploymentForm
type CreateDeploymentForm struct {
    // ... existing fields ...
    ExternalMCPs []AddExternalMCPForm `json:"externalMcps,omitempty"`
}

// In EvolveDeploymentForm
type EvolveDeploymentForm struct {
    // ... existing fields ...
    ExternalMCPs []UpsertExternalMCPForm `json:"externalMcps,omitempty"`
}

// AddExternalMCPForm specifies a new external MCP source
type AddExternalMCPForm struct {
    Name       string `json:"name"`       // Server name from registry (e.g., "ai.exa/exa")
    RegistryID string `json:"registryId"` // Internal registry UUID
}

// UpsertExternalMCPForm updates or creates an external MCP source
type UpsertExternalMCPForm struct {
    ID         *string `json:"id,omitempty"` // Existing source ID (for updates)
    Name       string  `json:"name"`         // Server name from registry
    RegistryID string  `json:"registryId"`   // Internal registry UUID
}
```

**Update `ListExternalMCPCatalogResponse`:**

```go
// ExternalMCPServer now includes internal RegistryID
type ExternalMCPServer struct {
    Name        string    `json:"name"`
    Version     string    `json:"version"`
    Description string    `json:"description"`
    RegistryID  string    `json:"registryId"`  // Internal UUID (not registry name)
    // ... rest of fields ...
}
```

### Schema Changes

Following the pattern of `deployments_openapiv3_assets`, `deployments_packages`, and `deployments_functions`, we introduce:

```sql
CREATE TABLE IF NOT EXISTS deployments_external_mcps (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  deployment_id uuid NOT NULL,
  registry_id uuid NOT NULL,

  -- Server identification from registry
  name TEXT NOT NULL,    -- Reverse-DNS name (e.g., "ai.exa/exa")
  version TEXT NOT NULL, -- Version from registry

  -- Metadata
  slug TEXT NOT NULL CHECK (slug <> '' AND CHAR_LENGTH(slug) <= 60),

  CONSTRAINT deployments_external_mcps_pkey PRIMARY KEY (id),
  CONSTRAINT deployments_external_mcps_deployment_id_fkey
    FOREIGN KEY (deployment_id) REFERENCES deployments (id) ON DELETE CASCADE,
  CONSTRAINT deployments_external_mcps_registry_id_fkey
    FOREIGN KEY (registry_id) REFERENCES registries (id) ON DELETE SET NULL,
  CONSTRAINT deployments_external_mcps_deployment_id_slug_key
    UNIQUE (deployment_id, slug)
);
```

**Rationale:**
- `registry_id` + `name` + `version` provide enough information to fetch server details from registry at runtime
- Connection details (URL, transport type) are resolved by querying the registry during source processing
- Transport type will always be remote (no need to store)
- `slug` follows existing pattern for user-facing identification

### Source Processing

**Background Workflow Extension:**

The existing `ProcessDeploymentWorkflow` in `server/internal/background/deployments.go` will be extended to handle external MCP sources alongside OpenAPI and Functions sources.

**Current workflow steps:**
1. TransitionDeployment → "pending"
2. ValidateDeployment (tier limits)
3. **ProcessDeployment** ← Extension point
4. ProvisionFunctionsAccess
5. DeployFunctionRunners
6. TransitionDeployment → "completed" or "failed"

**Extension to ProcessDeployment activity:**

In `server/internal/background/activities/process_deployment.go`, add `doExternalMCP()` method following the pattern of `doOpenAPIv3()` and `doFunctions()`:

```go
func (p *ProcessDeployment) doExternalMCP(
    ctx context.Context,
    pool *pool.ErrorPool,
    projectID uuid.UUID,
    deploymentID uuid.UUID,
    orgSlug string,
    projectSlug string,
    deployment *types.Deployment,
) error {
    for _, externalMCP := range deployment.ExternalMCPs {
        // Spawn goroutine in pool
        pool.Go(func() error {
            // Create extractor
            extractor := externalmcp.NewToolExtractor(p.logger, p.db, p.registryClient)

            // Validate server exists in registry
            // Fetch server connection details from registry
            // Create proxy tool definition
            // Record metrics

            return extractor.Do(ctx, externalmcp.ToolExtractorTask{
                ProjectID:    projectID,
                DeploymentID: deploymentID,
                ExternalMCP:  externalMCP,
                // ...
            })
        })
    }
    return nil
}
```

**Key behaviors:**
- Validation (server exists in registry) happens in `externalmcp.ToolExtractor`, not in `ValidateDeployment` activity
- Failure to validate marks source as failed (logged to deployment_events)
- Successful validation creates proxy tool definition(s)
- Errors are non-retryable (invalid server name is permanent failure)

### Tool Definition Tables

Following the pattern of OpenAPI (`http_tool_definitions`) and Functions (`function_tool_definitions`), external MCP sources will create tool definitions.

**Existing patterns:**
- **OpenAPI**: Creates `http_tool_definitions` via `CreateOpenAPIv3ToolDefinition` query (server/internal/deployments/queries.sql:408)
  - Stores HTTP method, path, schema, security, server URL
  - References `openapiv3_document_id` from `deployments_openapiv3_assets`
- **Functions**: Creates `function_tool_definitions` and `function_resource_definitions` via `CreateFunctionsTool` and `CreateFunctionsResource` queries
  - Stores runtime, input schema, variables
  - References `function_id` from `deployments_functions`

**Proposed for External MCP:**

```sql
CREATE TABLE IF NOT EXISTS external_mcp_tool_definitions (
  id uuid NOT NULL DEFAULT generate_uuidv7(),
  tool_urn TEXT NOT NULL,

  project_id uuid NOT NULL,
  deployment_id uuid NOT NULL,
  external_mcp_id uuid NOT NULL, -- References deployments_external_mcps

  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz,
  deleted boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) stored,

  CONSTRAINT external_mcp_tool_definitions_pkey PRIMARY KEY (id),
  CONSTRAINT external_mcp_tool_definitions_deployment_id_fkey
    FOREIGN KEY (deployment_id) REFERENCES deployments (id) ON DELETE CASCADE,
  CONSTRAINT external_mcp_tool_definitions_external_mcp_id_fkey
    FOREIGN KEY (external_mcp_id) REFERENCES deployments_external_mcps (id) ON DELETE CASCADE,
  CONSTRAINT external_mcp_tool_definitions_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS external_mcp_tool_definitions_deployment_id_tool_urn_key
  ON external_mcp_tool_definitions (deployment_id, tool_urn) WHERE deleted IS FALSE;
CREATE INDEX IF NOT EXISTS external_mcp_tool_definitions_external_mcp_id_idx
  ON external_mcp_tool_definitions (external_mcp_id) WHERE deleted IS FALSE;
```

**Note:** This table only stores the proxy tool registration. Tool metadata (name, description, input_schema) is resolved at runtime from the external MCP server.

**Note on MCP Resources:**

MCP resources are **out of scope for v0**. Resources seem confusing in an era before Gram apps exist - they expose server-specific data (like file contents, database records) without clear use cases in the current toolset model.

**Future work:** Create Linear ticket to attempt calling external MCP's `list_resources` endpoint. If resources are returned, add them to the server for future use.

**Extractor workflow:**
1. Resolve registry from database using `registry_id`
2. Query resolved registry URL to fetch server details using `name` field
3. Validate that specified `version` exists in registry response
4. Create single proxy tool with:
   - Tool name: `proxy`
   - Tool URN pattern: `tools:externalmcp:<slug>:proxy`
     - Type: `externalmcp`
     - Source slug: The `slug` field from `deployments_external_mcps`
     - Tool name: `proxy`
5. Store in `external_mcp_tool_definitions` with minimal metadata

**Proxy tool behavior:**
The proxy tool acts as a gateway. When Gram's MCP proxy receives a call to this tool, it will:
- Fetch actual tool list from external server at runtime
- Route tool calls to the appropriate external MCP server
- Handle tool discovery transparently (implementation details in separate MCP proxy work)

### Key Decision: Async Validation

External server validity is validated asynchronously in the background workflow rather than during the HTTP handler. This prevents external dependencies from blocking deployment creation.

## 3. Tool Calling & Proxy Architecture

This section covers how tool calls are routed through Gram to external MCP servers, with transparent proxying of `list_tools` and `call_tool`.

### Current MCP Architecture

**RPC Endpoints** (server/internal/mcp/):
- `rpc_tools_list.go:32` - Handles `tools/list` RPC
- `rpc_tools_call.go:49` - Handles `tools/call` RPC

**Tool Discovery Flow:**
1. Client calls `tools/list` on Gram toolset
2. `mv.DescribeToolset()` queries database for all tools in deployment
3. Returns tools with name, description, input schema

**Tool Calling Flow:**
1. Client calls `tools/call` with tool name + arguments
2. Find tool by name, extract URN (format: `tools:kind:source:name`)
3. `toolsets.GetToolCallPlanByURN()` switches on URN kind:
   - `http` → Query http_tool_definitions, build HTTP reverse proxy plan
   - `function` → Query function_tool_definitions, build serverless invocation plan
   - `prompt` → Query prompt templates, build template execution plan
4. `gateway.ToolProxy.Do()` executes the plan based on kind

**URN Structure:**
- Format: `tools:{kind}:{source}:{name}`
- Existing kinds: `http`, `function`, `prompt`
- New kind for external MCP: `externalmcp`

### External MCP Integration

#### Tool URN Format

External MCP tools use URN: `tools:externalmcp:<slug>:proxy`
- Kind: `externalmcp`
- Source: The `slug` from `deployments_external_mcps`
- Name: `proxy` (single proxy tool per external server)

#### list_tools Extension

**Location:** `server/internal/mcp/rpc_tools_list.go`

**Extended Flow:**
1. Query database for tools (includes proxy tools from `external_mcp_tool_definitions`)
2. Iterate over tools and detect `externalmcp` kind
3. For each external MCP proxy tool:
   - Look up `external_mcp_tool_definitions` record
   - Resolve `deployments_external_mcps` via `external_mcp_id`
   - Query registry for server connection details (URL, transport)
   - **Instantiate MCP client** pointing to external server
   - Call `client.ListTools()` through MCP protocol
   - Receive external tool list from remote server
   - **Prefix tool names** (e.g., `github:create_issue`, `github:list_repos`)
   - Flatten into overall tool list response
4. Return combined list of local tools + external tools

**Tool Name Prefixing:**
- Pattern: `{source-slug}:{original-tool-name}`
- Example: If external MCP has slug `github`, tools become `github:create_issue`, `github:list_repos`
- Ensures uniqueness across multiple external servers in same toolset

#### call_tool Extension

**Location:** `server/internal/toolsets/shared.go:42` (GetToolCallPlanByURN)

**Add new case:**
```go
case urn.ToolKindExternalMCP:
    // Query external_mcp_tool_definitions by URN
    tool, err := t.toolsRepo.GetExternalMCPToolByURN(ctx, ...)
    return t.extractExternalMCPToolCallPlan(ctx, tool)
```

**extractExternalMCPToolCallPlan workflow:**
1. Query `external_mcp_tool_definitions` by URN
2. Follow `external_mcp_id` to `deployments_external_mcps`
3. Query registry for server connection details
4. Return `gateway.NewExternalMCPToolCallPlan()` containing:
   - External server URL + transport
   - Original tool name (strip prefix)
   - Authentication details (if any)

**Location:** `server/internal/gateway/proxy.go:125` (ToolProxy.Do)

**Add new case:**
```go
case ToolKindExternalMCP:
    return tp.doExternalMCP(ctx, logger, w, requestBody, env, plan.Descriptor, plan.ExternalMCP, toolCallLogger)
```

**doExternalMCP implementation:**
1. Parse tool name from request (strip prefix to get original name)
2. **Instantiate MCP client** pointing to external server URL
3. Initialize MCP session (handshake, capabilities negotiation)
4. Call `client.CallTool(originalToolName, arguments)` through MCP protocol
5. Receive MCP response from external server
6. Check `isMCPPassthrough` (likely true for external MCP tools)
7. Return raw MCP response to client

### MCP Client Implementation

**New Package:** `server/internal/external_mcp/client.go`

**Responsibilities:**
- Implement MCP client protocol (JSON-RPC over HTTP/stdio/SSE)
- Handle MCP initialization handshake
- Support `list_tools`, `call_tool`, `list_resources`, `read_resource` methods
- Connection pooling/caching for repeated calls
- Timeout and retry logic

**Key Methods:**
```go
type MCPClient interface {
    Initialize(ctx context.Context) error
    ListTools(ctx context.Context) ([]MCPTool, error)
    CallTool(ctx context.Context, name string, args map[string]any) (*MCPToolResult, error)
    Close() error
}
```

### Response Passthrough

**Existing Infrastructure:**
- `mcp/passthrough.go:5` - `isMCPPassthrough(meta)` function
- Checks for `gram.ai/kind: "mcp-passthrough"` in tool meta
- Returns raw MCP response without content chunk formatting

**For External MCP:**
- External MCP tools will have `Meta: {"gram.ai/kind": "mcp-passthrough"}`
- This ensures raw MCP responses are returned transparently
- No additional formatting/wrapping by Gram

### Caching Strategy

**V0 Approach:**
- No caching of external `list_tools` responses
- Each `list_tools` call queries external server in real-time
- Connection pooling only (reuse MCP client connections)

**Future Considerations:**
- Cache `list_tools` responses with TTL
- Invalidate cache on deployment changes
- Background refresh of tool schemas

### Error Handling

**Registry Unavailable:**
- Log error, mark external MCP source as degraded
- Continue serving other tools in toolset
- Return partial tool list with warning

**External Server Unavailable:**
- During `list_tools`: Exclude external tools from response
- During `call_tool`: Return MCP error response to client
- Log to deployment events for debugging

**Version Mismatch:**
- If specified version doesn't exist in registry: Fail deployment processing
- Mark source as failed in deployment status

## 4. OAuth & Authentication

**Status:** Out of scope for v0

**Rationale:**
External MCP servers with OAuth do not represent a regression from the current system. The existing constraint ("no mixing multiple OAuth2 providers in a toolset") applies to current sources and will be designed/implemented separately from this work.

**Trivial Fallback Option:**
If OAuth becomes a blocker, we can implement a simple restriction: "Only one source allowed in any toolset containing an external MCP source." This ensures no OAuth conflicts but is overly restrictive.

**Preferred Approach:**
Design and implement homogeneous OAuth provider representation across all source types (OpenAPI, Functions, External MCP), then enforce the constraint uniformly. This work should happen in parallel or shortly after external MCP v0.

**Future Work:**
- Extract OAuth provider info from external MCP server metadata (from registry or server capabilities)
- Store OAuth provider per source
- Validate OAuth provider consistency when adding sources to toolsets
- Surface OAuth provider in UI when configuring external MCP sources

## User Experience

### Add Source Flow

**Entry Point:**

When users click "Add Source" in the Gram dashboard, they see a menu with options:
- From API Spec
- From Functions
- **Import External MCP** ← NEW

*Screenshot placeholder: "Add Source" button location in Gram UI*

**Import External MCP Dialog:**

1. **Browse Servers**
   - Shows list from `ListExternalMCPCatalog` RPC
   - Each server card displays: Title, Description, Status badge, Icon (if available)
   - Search/filter functionality
   - *Wireframe placeholder: Server selection grid*

2. **Configure Source**
   - User selects a server
   - Prompted to provide:
     - **Slug**: URL-friendly identifier (auto-generated from server name, editable)
   - Auth configuration (see OAuth section below)
   - *Wireframe placeholder: Configuration form*

3. **Add to Toolset**
   - Select which toolset (existing or create new)
   - Submit creates deployment with `AddExternalMCPForm`
   - Background processing validates and provisions source

**Feature Flag:** `external-mcp-catalog` in PostHog
- V0 shows "Enterprise feature - Contact sales" banner for orgs without flag
- Future: General availability

### Documentation Changes

*To be added: Links to setup guides, example integrations, troubleshooting*

## Billing

*How will this feature impact billing for customers ? Is it is a priced feature? If so which tier does it fall or does it require a rethink in existing pricing. Please check in with #sales for any impact to pricing.*

# Open Questions

*This section will contain any open questions that you come across writing your RFC or that are discovered during review:*

- **Version pinning vs. latest:** Should we specify the exact version at source creation time and pin to it, or always fetch the latest version from the registry when deploying? Pinning provides stability; latest provides automatic updates. Consider: breaking changes in external servers, user expectations, rollback scenarios.

- **Slug generation from reverse-DNS names:** The MCP registry uses reverse-DNS naming (e.g., "ai.exa/exa", "com.github/github"). Can we use these directly as source slugs with minimal sanitization (replace `/` with `-`)? Or do we need user-provided slugs? Consider: uniqueness within deployment, URL safety, user clarity.