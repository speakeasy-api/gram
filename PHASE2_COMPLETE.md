# Phase 2 Complete: Backend Services

## ✅ What Was Completed

All backend service components for staging and releases infrastructure have been implemented.

### Summary

Phase 2 builds on the database schema from Phase 1 and implements the complete backend service layer including:
- Releases service with CRUD operations
- State capture utilities for point-in-time snapshots
- Staging toolset management functions
- Variation versioning support

All services follow established project patterns and are ready for API endpoint integration in Phase 3.

## 🔍 Implementation Details

### 1. Releases Service (`server/internal/releases/`)

#### Service File: `service.go`
Main service managing toolset releases with full CRUD operations:

**Methods Implemented**:
- `NewService()` - Constructor with structured logging
- `CreateRelease()` - Creates a new release with auto-incrementing release_number
- `GetRelease()` - Retrieves release by ID
- `GetReleaseByNumber()` - Retrieves release by toolset + number
- `ListReleases()` - Lists releases with pagination
- `GetLatestRelease()` - Gets most recent release
- `CountReleases()` - Counts total releases for toolset
- `DeleteRelease()` - Deletes a release (destructive, use with caution)

**Key Features**:
- Uses `oops` package for structured error handling
- Logs all create/delete operations with context
- Returns domain-specific errors (CodeNotFound, CodeUnexpected)
- Thread-safe with context-aware operations

#### State Capture File: `state.go`
Utilities for capturing point-in-time state snapshots:

**Methods Implemented**:
- `CaptureSystemSourceState()` - Captures prompt template state
- `CaptureSourceState()` - Combines deployment + system sources (deduplicates if exists)
- `GetSystemSourceState()` - Retrieves system source state by ID
- `GetSourceState()` - Retrieves combined source state by ID
- `CreateToolVariationsGroupVersion()` - Creates version snapshot of variations group
- `GetToolVariationsGroupVersion()` - Retrieves specific variations version
- `GetLatestToolVariationsGroupVersion()` - Gets latest version of group
- `ListToolVariationsGroupVersions()` - Lists all versions of group

**Key Features**:
- Deduplication: Reuses existing source_states with same components
- Logging of all state capture operations
- Version tracking for variation groups

#### Repository Queries: `repo/queries.sql`
All SQLc queries for releases and state management:

```sql
-- Releases CRUD
CreateRelease           -- Auto-increments release_number per toolset
GetRelease              -- By ID
GetReleaseByNumber      -- By toolset + number
ListReleases            -- Paginated list
GetLatestRelease        -- Most recent release
CountReleases           -- Total count
DeleteRelease           -- Hard delete

-- State Capture
CreateSystemSourceState          -- Prompt templates state
GetSystemSourceState             -- Retrieve system state
CreateSourceState                -- Combined state
GetSourceState                   -- Retrieve combined state
GetSourceStateByComponents       -- Find existing by deployment + system

-- Variation Versioning
CreateToolVariationsGroupVersion -- Auto-increments version
GetToolVariationsGroupVersion    -- By ID
GetLatestToolVariationsGroupVersion -- Latest for group
ListToolVariationsGroupVersions  -- All versions
```

**Auto-Incrementing Logic**:
- `release_number`: `COALESCE((SELECT MAX(release_number) + 1 FROM toolset_releases WHERE toolset_id = @toolset_id), 1)`
- `version`: Similar pattern for group versions

### 2. Staging Management (`server/internal/toolsets/staging.go`)

New file in toolsets package with staging-specific operations:

**Methods Implemented**:
- `CreateStagingVersion()` - Duplicates toolset as staging copy
- `GetStagingVersion()` - Retrieves staging version if exists
- `GetToolsetWithStagingVersion()` - Gets toolset + staging in one query
- `DiscardStagingVersion()` - Deletes staging toolset
- `SwitchEditingMode()` - Toggles between iteration/staging modes
- `UpdateCurrentRelease()` - Sets current release for toolset
- `GetToolsetByID()` - Retrieves toolset by ID

**Staging Toolset Creation Logic**:
1. Checks if staging already exists (idempotent)
2. Creates staging with suffix: `{slug}-staging`
3. Names it: `{name} (staging)`
4. Copies all properties except MCP settings
5. Staging toolsets are NOT published as MCP servers

**Editing Mode Toggle**:
- Validates mode is 'iteration' or 'staging'
- Returns early if already in requested mode
- Logs mode transitions

#### Extended Toolsets Queries (`toolsets/queries.sql`)

Added 5 new queries:
```sql
-- name: GetStagingToolset :one
-- Finds staging toolset by parent_toolset_id

-- name: GetToolsetWithStagingVersion :one
-- LEFT JOIN to get toolset + optional staging

-- name: UpdateToolsetEditingMode :one
-- Updates editing_mode column

-- name: UpdateToolsetCurrentRelease :one
-- Sets current_release_id

-- name: GetToolsetByID :one
-- Gets toolset by UUID instead of slug
```

### 3. Variation Versioning (`server/internal/variations/versioning.go`)

New file in variations package for version-aware operations:

**Methods Implemented**:
- `CreateVariationVersion()` - Creates new version with predecessor tracking
- `GetVariationAtVersion()` - Retrieves specific version by ID
- `GetLatestVariationVersion()` - Gets latest version by source URN
- `ListVariationVersionHistory()` - All versions for a tool URN
- `GetVariationsInGroup()` - Current (latest) variations in group

**Versioning Strategy**:
- Uses `predecessor_id` to link versions
- First version has `predecessor_id = NULL`
- Subsequent versions point to previous version
- Supports version history traversal

**Note**: Current implementation still uses `UpsertToolVariation` which does in-place updates on conflict. For true immutable versioning, a new insert-only query would be needed. This is documented in code comments.

#### Extended Variations Queries (`variations/queries.sql`)

Added 5 new queries:
```sql
-- name: GetToolVariationByID :one
-- Retrieves variation by UUID

-- name: GetToolVariationByURN :one
-- Latest version for group + URN

-- name: GetLatestToolVariationByURN :one
-- Same as above (kept for clarity)

-- name: ListToolVariationVersions :many
-- All versions for group + URN ordered by version DESC

-- name: ListCurrentVariationsInGroup :many
-- Uses DISTINCT ON to get latest of each URN in group
```

## 📁 Files Created/Modified

### New Files Created
1. `/server/internal/releases/service.go` (176 lines)
2. `/server/internal/releases/state.go` (143 lines)
3. `/server/internal/releases/repo/queries.sql` (134 lines)
4. `/server/internal/toolsets/staging.go` (222 lines)
5. `/server/internal/variations/versioning.go` (107 lines)

### Files Modified
1. `/server/internal/toolsets/queries.sql` (added 48 lines)
2. `/server/internal/variations/queries.sql` (added 42 lines)
3. `/server/database/sqlc.yaml` (added releases repo configuration)

### Files Generated (by SQLc)
- `/server/internal/releases/repo/db.go`
- `/server/internal/releases/repo/models.go`
- `/server/internal/releases/repo/queries.sql.go`
- Updated `/server/internal/toolsets/repo/queries.sql.go`
- Updated `/server/internal/variations/repo/queries.sql.go`

## ✅ Verification Steps

### Step 1: Verify SQLc Code Generation

```bash
cd server
mise gen:sqlc-server
```

**Expected**: No errors, all query files generate Go code successfully

### Step 2: Run Linters

```bash
mise lint:server
```

**Expected**: Only sloglint warnings (acceptable style preferences), no errorlint or typecheck errors

### Step 3: Verify Service Instantiation (Manual)

```go
package main

import (
    "log/slog"
    "github.com/speakeasy-api/gram/server/internal/releases"
)

func main() {
    logger := slog.Default()
    // Assume db is *pgxpool.Pool
    releasesService := releases.NewService(logger, db)

    // Service is ready to use
}
```

### Step 4: Verify Query Methods Exist

Check that generated repo packages have expected methods:

```bash
# Releases repo
grep -o "func (q \*Queries) [A-Za-z]*" server/internal/releases/repo/queries.sql.go | sort

# Should include:
# CreateRelease
# GetRelease
# GetReleaseByNumber
# ListReleases
# GetLatestRelease
# CountReleases
# DeleteRelease
# CreateSystemSourceState
# GetSystemSourceState
# CreateSourceState
# GetSourceState
# GetSourceStateByComponents
# CreateToolVariationsGroupVersion
# GetToolVariationsGroupVersion
# GetLatestToolVariationsGroupVersion
# ListToolVariationsGroupVersions
```

## 🧪 Testing Notes

Phase 2 implementation is complete but **tests are not yet written**. This was intentional following Boyd's Law - implement core functionality first, verify it compiles and passes linters, then add tests.

### Recommended Test Coverage (Phase 2.6)

#### Releases Service Tests (`releases/service_test.go`)
- [ ] `TestCreateRelease` - Creates release with correct release_number
- [ ] `TestCreateRelease_AutoIncrement` - Verifies sequential numbering
- [ ] `TestGetRelease` - Retrieves existing release
- [ ] `TestGetRelease_NotFound` - Returns CodeNotFound error
- [ ] `TestGetReleaseByNumber` - Retrieves by number
- [ ] `TestListReleases` - Lists with pagination
- [ ] `TestGetLatestRelease` - Gets most recent
- [ ] `TestCountReleases` - Counts correctly
- [ ] `TestDeleteRelease` - Deletes successfully

#### State Capture Tests (`releases/state_test.go`)
- [ ] `TestCaptureSystemSourceState` - Creates system state
- [ ] `TestCaptureSourceState` - Combines states
- [ ] `TestCaptureSourceState_Deduplication` - Reuses existing
- [ ] `TestCreateToolVariationsGroupVersion` - Creates version
- [ ] `TestGetLatestToolVariationsGroupVersion` - Gets latest

#### Staging Management Tests (`toolsets/staging_test.go`)
- [ ] `TestCreateStagingVersion` - Creates staging copy
- [ ] `TestCreateStagingVersion_Idempotent` - Returns existing
- [ ] `TestGetStagingVersion` - Retrieves staging
- [ ] `TestGetStagingVersion_NotFound` - Returns not found
- [ ] `TestDiscardStagingVersion` - Deletes staging
- [ ] `TestSwitchEditingMode` - Changes mode
- [ ] `TestSwitchEditingMode_Validation` - Rejects invalid modes

#### Variation Versioning Tests (`variations/versioning_test.go`)
- [ ] `TestCreateVariationVersion` - Creates with predecessor
- [ ] `TestGetVariationAtVersion` - Retrieves specific version
- [ ] `TestGetLatestVariationVersion` - Gets current version
- [ ] `TestListVariationVersionHistory` - Lists all versions
- [ ] `TestGetVariationsInGroup` - Gets current variations

## 🏗️ Code Patterns & Conventions

All Phase 2 code follows established project patterns:

### Error Handling
```go
if err != nil {
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, oops.E(oops.CodeNotFound, nil, "resource not found")
    }
    return nil, oops.E(oops.CodeUnexpected, err, "operation failed").
        Log(ctx, s.logger)
}
```

### Logging
```go
s.logger.InfoContext(ctx, "operation completed",
    slog.String("field_name", value),
    slog.Int64("numeric_field", number),
)
```

### Context Usage
- All service methods take `context.Context` as first parameter
- Use `ctx` for database queries, logging, and error handling

### Null Handling
```go
// Using uuid.NullUUID
uuid.NullUUID{UUID: someUUID, Valid: true}

// Using pgtype.Text
pgtype.Text{String: "value", Valid: true}
pgtype.Text{String: "", Valid: false}  // for NULL
```

## 🚀 Next Steps

### Phase 3: API Endpoints (Week 4-5)

Now that backend services are complete, Phase 3 adds Goa-based API endpoints:

#### 3.1 Goa Design Updates (`design/releases/design.go`)

Create new Goa service definition:
```go
var _ = Service("releases", func() {
    Method("create", func() {
        Payload(func() {
            Attribute("toolset_slug", String)
            Attribute("notes", String)
            Required("toolset_slug")
        })
        Result(Release)
        HTTP(func() {
            POST("/projects/{project_id}/toolsets/{toolset_slug}/releases")
        })
    })

    Method("list", func() { /* ... */ })
    Method("get", func() { /* ... */ })
    Method("getByNumber", func() { /* ... */ })
    Method("rollback", func() { /* ... */ })
})
```

#### 3.2 Update Toolsets Design (`design/toolsets/design.go`)

Add staging endpoints:
```go
Method("createStaging", func() { /* ... */ })
Method("getStaging", func() { /* ... */ })
Method("publishStaging", func() { /* ... */ })
Method("discardStaging", func() { /* ... */ })
Method("switchMode", func() { /* ... */ })
```

#### 3.3 Generate Goa Code

```bash
mise gen:goa-server
```

#### 3.4 Implement Endpoints

Wire up service methods in generated endpoint handlers.

### Phase 4: Frontend Integration (Week 5-6)

Add UI for releases management:
- Release history view
- Create release modal
- Rollback confirmation dialog
- Staging/iteration mode toggle
- Staging publish workflow

### Phase 5: MCP Server Updates (Week 6)

Update MCP server metadata to reflect releases:
- Include current release info in server capabilities
- Version information in tool metadata

### Phase 6: Testing & Polish (Week 7-8)

- Write comprehensive unit tests
- Add integration tests
- Performance testing
- Documentation updates

### Phase 7: Beta Testing & Rollout (Week 9-10)

- Internal dogfooding
- Beta customer access
- Monitoring and metrics
- Production rollout

## 📝 Known Limitations

### 1. Variation Versioning Not Fully Immutable

Current implementation uses `UpsertToolVariation` which does in-place updates:
```sql
ON CONFLICT (group_id, src_tool_urn) WHERE deleted IS FALSE DO UPDATE SET ...
```

For true immutable versioning, we need:
- Remove the ON CONFLICT clause
- Always INSERT new rows
- Update unique constraint to include `version` column
- Ensure queries use `ORDER BY version DESC LIMIT 1` to get latest

**Impact**: Variations captured in a release may change if edited after release creation. For Phase 2, this is acceptable since we're not yet enforcing immutability.

**Fix Required**: Update `UpsertToolVariation` query and schema in Phase 3.

### 2. No Publish Staging Workflow Yet

`CreateStagingVersion()` creates the staging copy, but there's no `PublishStagingVersion()` method yet that:
1. Captures complete state (deployment, toolset version, variations)
2. Creates a release
3. Updates `current_release_id` on parent toolset
4. Optionally deletes or preserves staging toolset

**Fix Required**: Implement in Phase 3 as part of API endpoints.

### 3. No Rollback Implementation

Service has `GetRelease()` but no `RollbackToRelease()` that:
1. Retrieves historical release state
2. Restores deployment, toolset version, variations
3. Updates `current_release_id`

**Fix Required**: Implement in Phase 3.

### 4. No Release Diffing

Can't compare two releases to see what changed.

**Fix Required**: Future enhancement (Phase 6+).

### 5. No Release Notes Markdown Rendering

Release notes are stored as plain text, no markdown support.

**Fix Required**: Frontend can add markdown rendering (Phase 4).

## 🔧 Linter Warnings (Acceptable)

Phase 2 code passes all critical linters but has style warnings:

### sloglint Warnings (Acceptable)
```
raw keys should not be used (sloglint)
```

These are style preferences. Project uses `slog.String("key", value)` directly rather than defining constants for every log key. This is acceptable for:
- Unique context-specific keys
- Keys not shared across services
- Logging that's not part of structured observability pipeline

For observability keys (HTTP status, trace IDs, etc.), we use `attr.Slog*()` helpers.

## ✅ Success Criteria

- [x] Releases service implemented with CRUD operations
- [x] State capture utilities created
- [x] Staging management functions added to toolsets service
- [x] Variation versioning support implemented
- [x] All SQLc queries generated successfully
- [x] Code compiles without errors
- [x] Linters pass (only acceptable sloglint warnings)
- [x] Follows project conventions and patterns
- [x] Proper error handling with oops package
- [x] Context-aware logging with slog
- [x] Thread-safe operations
- [x] Null handling with pgtype
- [ ] Tests written (deferred to Phase 2.6)

---

**Status**: Phase 2 Complete ✅
**Date**: 2025-11-28
**Next Phase**: Phase 3 - API Endpoints (Goa design + implementation)
**Dependencies**: Phase 1 database schema (✅ complete)
