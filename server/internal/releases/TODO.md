# Releases Implementation - Remaining TODOs

This document outlines the remaining work needed to complete the full implementation of the releases feature for toolsets.

## Overview

The current implementation provides a working skeleton for the releases system with placeholder implementations for state capture and rollback. The following items need to be completed for a production-ready implementation.

## State Capture (CreateRelease)

**Location**: `internal/releases/impl.go` lines 82-130

### Current Status
- ✅ Creates toolset_version records with proper versioning
- ✅ Creates release records with proper release numbering
- ✅ Handles optional state IDs (source_state_id, variations)
- ❌ Does not capture actual toolset state

### Required Implementation

#### 1. Capture Toolset Version State
Currently creates empty toolset_versions. Need to:
- Get the current toolset's tool URNs from toolset_tools junction table
- Get the current toolset's resource URNs from toolset_resources junction table
- Populate the `ToolUrns` and `ResourceUrns` fields in CreateToolsetVersionParams

**Code Location**: `impl.go:93-115`

```go
// TODO: Get actual tool_urns and resource_urns from current toolset state
toolsetVersion, err := s.toolsetsRepo.CreateToolsetVersion(ctx, toolsetsRepo.CreateToolsetVersionParams{
    ToolsetID:     toolset.ID,
    Version:       nextVersion,
    ToolUrns:      []urn.Tool{},      // <- POPULATE FROM ACTUAL TOOLSET
    ResourceUrns:  []urn.Resource{},  // <- POPULATE FROM ACTUAL TOOLSET
    PredecessorID: uuid.NullUUID{Valid: false},
})
```

**Required Queries**:
- Add query to get current tool URNs for a toolset
- Add query to get current resource URNs for a toolset

#### 2. Capture Source State
Currently passes NULL for source_state_id. Need to:
- Get the current deployment for the project
- Capture system source state (prompt templates, system configs)
- Create a source_state record that combines:
  - Deployment snapshot
  - System configuration snapshot
- Use the created source_state ID when creating the release

**Code Location**: `impl.go:118-126`

```go
release, err := s.releasesService.CreateRelease(ctx, CreateReleaseParams{
    ToolsetID:                  toolset.ID,
    SourceStateID:              uuid.NullUUID{Valid: false}, // <- CAPTURE ACTUAL STATE
    ToolsetVersionID:           toolsetVersion.ID,
    GlobalVariationsVersionID:  uuid.NullUUID{Valid: false}, // <- CAPTURE IF EXISTS
    ToolsetVariationsVersionID: uuid.NullUUID{Valid: false}, // <- CAPTURE IF EXISTS
    Notes:                      payload.Notes,
    ReleasedByUserID:           authCtx.UserID,
})
```

**Required Implementation**:
- Define source_states table schema (if not exists)
- Implement state capture logic
- Store deployment snapshots
- Store system configuration snapshots

#### 3. Capture Variations Versions
Currently passes NULL for both variations version IDs. Need to:
- Query for global variations (organization-scoped)
- Query for toolset-specific variations
- Create variation version snapshots if they exist
- Pass the version IDs when creating the release

**Required Queries**:
- Add query to get global variations for organization
- Add query to get toolset-specific variations
- Implement variations versioning system

## Rollback Implementation

**Location**: `internal/releases/impl.go` lines 226-278

### Current Status
- ✅ Gets the release to rollback to
- ✅ Updates toolset.current_release_id
- ❌ Does not restore any actual state

### Required Implementation

#### 1. Restore Deployment State
Need to:
- Get the source_state_id from the release
- Retrieve the deployment snapshot from source_state
- Restore or recreate the deployment to match the snapshot
- This may involve creating a new deployment or updating the existing one

**Code Location**: `impl.go:247-253`

```go
// TODO: Implement rollback logic
// This would:
// 1. Restore deployment from source_state_id      <- IMPLEMENT
// 2. Restore toolset version from toolset_version_id
// 3. Restore variations from variation version IDs
// 4. Update toolset.current_release_id to this release
```

#### 2. Restore Toolset Version
Need to:
- Get the toolset_version_id from the release
- Retrieve the tool_urns and resource_urns
- Update the toolset's current tools and resources to match the version
- Clear tools/resources not in the version
- Add tools/resources that are in the version

**Considerations**:
- Should we modify the existing toolset or create a new snapshot?
- How do we handle deleted tools/resources?
- Should rollback be instant or queued?

#### 3. Restore Variations
Need to:
- Get global_variations_version_id from release (if not null)
- Get toolset_variations_version_id from release (if not null)
- Restore the variation configurations to match the snapshots
- This affects:
  - Prompt templates
  - Model configurations
  - Other toolset-specific settings

#### 4. Update Current Release Pointer
Currently implemented - updates the current_release_id on the toolset.

**Code Location**: `impl.go:255-261` - ✅ Already implemented

## Version Number Management

**Location**: `internal/releases/impl.go` lines 92-102

### Current Status
- ✅ Queries for latest version and increments
- ✅ Handles first version (starts at 1)
- ⚠️  Basic implementation, may need improvement for concurrent access

### Potential Improvements

#### 1. Concurrent Safety
The current implementation has a race condition:
```go
latestVersion, err := s.toolsetsRepo.GetLatestToolsetVersion(ctx, toolset.ID)
if err == nil {
    nextVersion = latestVersion.Version + 1
}
```

**Issue**: Between getting the latest version and creating the new version, another process could create a version, leading to a unique constraint violation.

**Solutions**:
- Use database-level auto-increment or sequence
- Add retry logic with exponential backoff
- Use SELECT FOR UPDATE or advisory locks
- Handle unique constraint errors and retry

#### 2. Predecessor Tracking
Currently sets predecessor_id to NULL for all versions. Should:
- Set predecessor_id to the previous version's ID
- This enables version history traversal
- Useful for diff operations and understanding version evolution

## Testing Gaps

### Integration Tests Needed

1. **State Capture Tests**
   - Test capturing complete toolset state
   - Test capturing with multiple tools and resources
   - Test capturing with variations present
   - Test capturing source state snapshots

2. **Rollback Tests**
   - Test rolling back to previous release
   - Test rollback with state restoration
   - Test rollback with variations
   - Test rollback to very old releases (skip multiple versions)

3. **Concurrent Access Tests**
   - Test multiple simultaneous release creations
   - Test version number conflicts
   - Test race conditions in rollback

4. **Edge Cases**
   - Test rollback when source state is missing
   - Test rollback when toolset has been modified since release
   - Test creating release with no tools/resources
   - Test rollback to first release

## Database Schema Considerations

### Missing Tables

The following referenced tables may need to be created:

1. **source_states** - For storing deployment + system snapshots
   ```sql
   CREATE TABLE source_states (
     id UUID PRIMARY KEY,
     deployment_snapshot JSONB NOT NULL,
     system_snapshot JSONB NOT NULL,
     created_at TIMESTAMPTZ NOT NULL DEFAULT now()
   );
   ```

2. **variation_versions** - For storing variation snapshots
   ```sql
   CREATE TABLE variation_versions (
     id UUID PRIMARY KEY,
     scope VARCHAR NOT NULL, -- 'global' or 'toolset'
     toolset_id UUID, -- NULL for global
     config JSONB NOT NULL,
     created_at TIMESTAMPTZ NOT NULL DEFAULT now()
   );
   ```

### Foreign Key Constraints

Review and potentially adjust:
- `toolset_releases_source_state_id_fkey` - Currently causes issues with NULL values
- Consider making source_state_id nullable or adding a default "empty" state

## Documentation Needed

1. **API Documentation**
   - Document the full release workflow
   - Document rollback behavior and limitations
   - Document what state is captured in a release

2. **Architecture Documentation**
   - Document the versioning strategy
   - Document state capture mechanism
   - Document rollback process flow

3. **User Guide**
   - How to create releases
   - When to use releases vs iterations
   - How to rollback safely
   - Best practices for release management

## Priority Order

Recommended implementation order:

1. **High Priority** (Required for MVP):
   - [ ] Capture actual tool/resource URNs in toolset_version
   - [ ] Implement proper version numbering with concurrency handling
   - [ ] Add retry logic for version conflicts

2. **Medium Priority** (Required for beta):
   - [ ] Implement source state capture
   - [ ] Implement basic rollback (tools/resources only)
   - [ ] Add integration tests for state capture

3. **Lower Priority** (Nice to have):
   - [ ] Implement variations capture and restore
   - [ ] Add predecessor tracking
   - [ ] Implement full rollback with all state
   - [ ] Add comprehensive edge case tests

## Estimated Effort

- **State Capture Implementation**: 2-3 days
  - Tool/Resource URNs: 0.5 days
  - Source State: 1 day
  - Variations: 0.5-1 day
  - Testing: 0.5-1 day

- **Rollback Implementation**: 2-3 days
  - Deployment restoration: 1 day
  - Toolset version restoration: 0.5 day
  - Variations restoration: 0.5-1 day
  - Testing: 0.5-1 day

- **Concurrency & Robustness**: 1 day
  - Version number handling: 0.5 day
  - Error handling & retry logic: 0.5 day

**Total Estimate**: 5-7 days for complete implementation

## Notes

- The current implementation is suitable for testing and development
- All tests pass with placeholder implementations
- The API interface is complete and stable
- Focus should be on state capture first, then rollback
- Consider implementing incrementally: tools/resources first, then source state, then variations
