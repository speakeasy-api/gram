# RFC: External Servers - Execution Plan

This document tracks research, design decisions, and implementation progress for adding external MCP server registry support to Gram.

**Last Updated:** 2025-11-24 (post-RFC section 1 & 2 decisions)

---

## Document Purpose

This is a living document that:
- Organizes work by concept/feature area rather than task type
- Tracks research findings and decisions as they're made
- Helps rebuild context after compactions or handoffs
- Records open questions and blockers

---

## Quick Reference

### Key Terminology
- **External Server**: MCP server hosted outside Gram (e.g., from MCP registry)
- **Registry**: External catalog of MCP servers (e.g., PulseMCP)
- **Proxy Tool**: Single tool like `tools:external_mcp:github:proxy` that routes to external server
- **Tool Flattening**: Expanding proxy tool into actual tools during `list_tools` RPC

### Useful Commands
```bash
# Database operations
mise db:diff <migration-name>    # Generate migration after schema changes
mise db:migrate                  # Apply pending migrations

# Code generation
mise gen:sqlc-server            # Generate SQLc code from queries
mise gen:goa-server             # Generate Goa code from design files

# Development
mise start:server --dev-single-process   # Run server locally
mise lint:server                # Run server linters
mise build:server               # Build server binary
```

### Context Rebuild for Subagents
When spawning subagents for research tasks, provide:
1. This execution plan document
2. Relevant PRD/design doc sections from Notion
3. Specific concept section they're researching

---

## Core Concepts

### 1. Listing Available Servers from Registries

**Goal:** Store and manage external MCP registries (like PulseMCP) that organizations can access, and provide UI for browsing available external MCP servers.

#### Schema Design

**Tables to create:**
- `registries`: Stores registry metadata
  - `id` (UUID, primary key)
  - `name` (string)
  - `url` (string) - API endpoint for registry
  - `created_at`, `updated_at`, `deleted_at`, `deleted` (standard columns)
  - Open questions:
    - What other metadata do we need? (API key? rate limits?)
    - Versioning?

- `registry_organizations`: Join table for org access control
  - `registry_id` (UUID, FK to registries)
  - `organization_id` (UUID, FK to organizations)
  - `created_at`, `updated_at`, `deleted_at`, `deleted`
  - Open questions:
    - Do we need role/permission levels per org?
    - Admin-only access vs. consumer access?

#### API Design

**New RPC method:**
- `ListExternalMCPCatalog(organization_id) → ListExternalMCPCatalogResponse`
  - Proxies to external registry API (MCP Registry at https://registry.modelcontextprotocol.io/v0.1/servers)
  - Returns list of available MCP servers
  - Runtime call (not cached for v0)
  - **Decision:** No caching for v0. Linear ticket to be created for future caching implementation.
  - **Decision:** Only servers with `remotes[]` are supported (packages filtered out)

**Response structure:**
```go
type ListExternalMCPCatalogResponse struct {
    Servers    []ExternalMCPServer
    NextCursor *string
}

type ExternalMCPServer struct {
    Name        string    // "ai.exa/exa"
    Version     string    // "1.0.0"
    Description string
    RegistryID  string    // Internal UUID
    Status      string    // "active", "deprecated", "deleted"
    UpdatedAt   time.Time
    Title       *string
    IconURL     *string
}
```

  - Open questions:
    - Pagination implementation details?
    - Client-side search or server-side filtering?
    - Partial results when registry is down?

#### Admin Management (v0)

- SQL-only admin for v0
- Manual INSERT into `registries` and `registry_organizations`
- No UI for registry management in v0
- Future: Admin UI for managing registries

#### Research Needed
- [ ] Review PulseMCP API documentation
- [ ] Determine what metadata is available per external server
- [ ] Understand auth requirements for registry APIs
- [ ] Design error handling strategy for unavailable registries

#### Design Artifacts Needed
- [ ] Schema diagram: `registries` + `registry_organizations` tables
- [ ] Sequence diagram: User requests external servers list → Registry proxy → Response
- [ ] API spec for `externalRegistriesList` RPC

---

### 2. Source Creation from External Registry

**Goal:** Allow users to select an external MCP server from registry and create a Gram source that proxies to it.

#### Source Representation

**Decision made:** Store minimal reference data, fetch connection details at runtime

**Table:** `deployments_external_mcps`
- `id` (UUID)
- `deployment_id` (FK to deployments)
- `registry_id` (FK to registries)
- `name` (TEXT) - Reverse-DNS name (e.g., "ai.exa/exa")
- `version` (TEXT) - Version from registry
- `slug` (TEXT) - User-facing identifier

**Rationale:**
- `registry_id` + `name` + `version` provide enough to fetch server details at runtime
- Connection details (URL, transport) resolved by querying registry during processing
- Transport type always remote (no need to store)
- Follows pattern of `deployments_openapiv3_assets`, `deployments_packages`, `deployments_functions`

**Open questions:**
- How do we handle registry changes?
  - What if server is removed from registry?
  - What if server URL changes in registry?
- Error handling when registry is unavailable during tool calls?

#### Integration with Deployment Create/Evolve

**Decision made:** Accept `name` + `registryID` in forms, validate asynchronously

**Goa Design Changes:**
- `CreateDeploymentForm.ExternalMCPs` - array of `AddExternalMCPForm`
- `EvolveDeploymentForm.ExternalMCPs` - array of `UpsertExternalMCPForm`
- `AddExternalMCPForm`: `name` (string), `registryId` (string)
- `UpsertExternalMCPForm`: `id?` (string), `name` (string), `registryId` (string)

**Async validation workflow:**
1. HTTP handler accepts forms and creates deployment record
2. Background workflow validates server exists in registry
3. If invalid, mark source as failed with error details
4. If valid, proceed with source processing

**Rationale:** Prevents external dependencies from blocking deployment operations

**Research still needed:**
- [ ] How does `create deployment` currently handle different source types?
- [ ] Where does source → tool mapping happen?
- [ ] How do we integrate external MCP processing into existing workflow?

#### User Experience Flow

**"Import External MCP" option in "Add Source" interfaces:**

1. User clicks "Add Source" → sees options:
   - From API Spec
   - From Functions
   - **Import External MCP** ← NEW

2. Clicking "Import External MCP" opens dialog:
   - Lists available external servers from `externalRegistriesList`
   - Shows metadata: name, description, quality score, last tested
   - Displays "Enterprise feature - Click to request" banner for v0 (feature flagged)

3. User selects server → Configure:
   - Auth setup (see OAuth section)
   - Name/description for this source
   - Which toolset to add to (new or existing)

4. Creates source + adds to toolset

**Feature flag:** `external-mcp-catalog` in PostHog

#### Design Artifacts Needed
- [ ] Wireframe: "Add Source" menu with External MCP option
- [ ] Wireframe: External server selection dialog
- [ ] Wireframe: External server configuration flow
- [ ] Sequence diagram: Full import flow from selection to source creation

---

### 3. Tool Calling & Proxy Architecture

**Goal:** Route tool calls through Gram to external MCP servers, with transparent proxying of `list_tools` and `call_tool`.

#### Proxy Tool Pattern

**Concept:**
- External server source has single tool: `tools:external_mcp:<server-id>:proxy`
- During `list_tools` RPC, proxy tool gets "flattened"
- Flattening: Call external server's `list_tools`, return those tools with metadata to route back

**Tool metadata structure:**
- Original tool name from external server
- Source ID (to route calls back)
- Any other routing info needed?

**Tool calling flow:**
1. Client calls `list_tools` on Gram toolset
2. Gram sees external source with proxy tool
3. Gram calls external server's `list_tools`
4. Gram returns flattened list with routing metadata
5. Client calls tool (with routing metadata)
6. Gram routes to correct external server via source ID
7. Gram proxies call to external server
8. Returns response

#### Current System Research

**Need to understand:**
- [ ] How are tool calls currently mapped to Tool URNs?
  - Where does this mapping happen?
  - What's the data structure?
  - Code locations?

- [ ] How do Tool URNs map back to sources?
  - Lookup mechanism?
  - Caching?
  - Performance considerations?

- [ ] Where does tool call routing happen?
  - Entry point for tool calls?
  - How are different source types handled?
  - Can we inject proxy logic cleanly?

**Files to examine:**
- `server/internal/mcp/` - MCP RPC implementations
- Tool call handling logic
- Source → Tool relationship

#### Proxy Implementation

**New logic needed:**
- Detect external source type during `list_tools`
- Proxy to external server's `list_tools`
- Transform/flatten response with routing metadata
- Route `call_tool` to external server based on metadata
- Error handling for external server failures

**Open questions:**
- Caching strategy for external `list_tools`?
- Timeout handling?
- Retry logic?
- How do we handle external server version changes?

#### Design Artifacts Needed
- [ ] Sequence diagram: `list_tools` with external source flattening
- [ ] Sequence diagram: `call_tool` routing to external server
- [ ] Code diagram: Tool URN → Source mapping (current + proposed)

---

### 4. OAuth & Auth Provider Constraint

**Goal:** Ensure external MCP servers declare their OAuth provider consistently with existing sources, and enforce "no mixing multiple OAuth2 providers in a toolset" constraint.

#### Current System Research

**Need to understand:**
- [ ] How do API spec sources declare OAuth provider?
  - Where is this stored in database?
  - How is it extracted from OpenAPI specs?
  - Code locations?

- [ ] How do Function sources declare OAuth provider?
  - Configuration mechanism?
  - Storage?

- [ ] Where is OAuth provider info surfaced to users?
  - UI locations?
  - API responses?

- [ ] Is there any existing constraint checking?
  - Or is this entirely net new?

**Files to examine:**
- OpenAPI parsing for OAuth extraction
- Source schema and OAuth-related columns
- Function source configuration
- Toolset validation logic

#### OAuth Provider Representation

**Goal:** Homogeneous representation across all source types

**Schema additions needed:**
- How is OAuth provider stored per source?
- Do we need a separate `oauth_providers` table?
- Or is it a column on sources?

#### Constraint Enforcement

**Product constraint:** Cannot create/edit toolset with sources from multiple OAuth2 providers

**Implementation needs:**
- Validation logic when adding source to toolset
- Validation logic when editing toolset
- User-facing error messages
- UI to show which OAuth provider is "active" for a toolset

**Open questions:**
- What if source has no OAuth? Does it count as a provider?
- What about API key auth vs. OAuth?
- Should constraint be "no mixing auth providers" or specifically OAuth2?

#### External Server OAuth Config

**Challenge:** External servers have their own OAuth providers

**Approach (TBD based on research):**
- Extract OAuth provider info from external server metadata?
- Or from PulseMCP registry data?
- Store with source record
- Use in constraint checking

#### Design Artifacts Needed
- [ ] Diagram: OAuth provider representation across source types
- [ ] Wireframe: Toolset with OAuth constraint error message
- [ ] Sequence diagram: Constraint validation flow

#### Separate Treatment in RFC

This concept gets its own sections in:
- **Goals:** Support external servers with diverse auth
- **Proposal > OAuth Integration:** Technical approach
- **User Experience > OAuth Constraints:** How users experience this

---

### 5. Local Development Environment

**Goal:** Full local dev experience with mock registry and ability to populate it with test data.

#### Mock Registry Implementation

**Add to docker-compose:**
- Mock MCP registry service
- Mimics PulseMCP API
- Serves test/sample external servers
- Configurable via local files

**Implementation options:**
- Simple HTTP server (Go/Node/Python)
- Mock server config files
- Or use actual PulseMCP in dev?

#### Mise Task: Populate Registry

**Command:** `mise dev:populate-registry <project-slug>`

**Behavior:**
1. Fetch toolsets from specified Gram project
2. Register them in local mock registry
3. Makes them available via `externalRegistriesList`

**Use case:** Developer can quickly populate registry with real Gram toolsets for testing import flow

**Open questions:**
- How do we transform Gram toolsets to registry format?
- Do we need auth/API keys in local dev?
- Should this be idempotent?

#### Zero Setup Integration

- Add mock registry to `zero` setup
- Pre-populate with sample servers
- Document in README

#### Design Artifacts Needed
- [ ] Mock registry API spec
- [ ] Docker-compose configuration additions
- [ ] Mise task implementation spec

---

## Open Questions & Blockers

### Critical Path Questions
1. ~~**Source reference strategy:** How do we reference external servers?~~ **RESOLVED:** Store `registry_id` + `name` + `version`, fetch connection details at runtime
2. **Registry sync:** How do we handle changes to external server definitions in registry?
3. **OAuth provider extraction:** How do we get OAuth info from external servers?
4. **Source processing:** How do we integrate external MCP processing into the existing deployment workflow?

### Research Dependencies
- PulseMCP API documentation and capabilities
- Current OAuth provider representation (blocks constraint implementation)
- Current Tool URN mapping (blocks proxy architecture)

### Design Decisions Needed
- Error handling strategy for unavailable registries/servers
- Caching strategy for external `list_tools`
- Version tracking for external server definitions

---

## Progress Tracking

### Completed Research
- [x] MCP Registry API structure (via subagent)
- [ ] OAuth provider current implementation
- [ ] Tool URN → Source mapping
- [ ] Deployment create/evolve flow
- [x] Schema pattern for deployment sources (deployments_openapiv3_assets, etc.)

### Completed Design Artifacts
- [x] Schema: `registries` and `registry_organizations` tables
- [x] Schema: `deployments_external_mcps` table
- [x] Go types: `ListExternalMCPCatalogResponse` and `ExternalMCPServer`
- [x] Go types: `AddExternalMCPForm` and `UpsertExternalMCPForm`
- [x] Sequence diagram: Registry listing flow
- [ ] Wireframes: Import flow (placeholders added)
- [ ] Sequence diagram: Source creation flow
- [ ] Sequence diagram: `list_tools` with external source flattening
- [ ] Sequence diagram: `call_tool` routing to external server

### Implementation Status
_To be filled in as work progresses_

---

## Appendix: Related Documents

- **PRD:** [PRD: 3P MCP Servers & Gram Catalog v0](https://www.notion.so/2b0726c497cc80139a3cf2fde2d7d0ea)
- **RFC:** `rfc-external-servers.md` (this repo)
- **Slack Discussion:** Multiple OAuth providers constraint (2025-11-24)

---

## Notes for Future Self / Subagents

### When Resuming Work
1. Review "Critical Path Questions" for current blockers
2. Check "Research Dependencies" for next tasks
3. Review most recent updates at top of relevant concept section

### When Spawning Research Subagents
Provide:
- This entire document (for context)
- Specific concept section they're researching
- Related Notion doc excerpts if relevant
- Clear deliverable (findings, diagram, code locations)

### When Making Decisions
Update relevant concept section with:
- Decision made
- Rationale
- Alternatives considered
- Impact on other concepts
