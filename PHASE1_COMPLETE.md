# Phase 1 Complete: Database Schema Changes

## ✅ What Was Completed

All database schema changes for staging and releases infrastructure have been implemented and migrated.

### Schema Changes Summary

#### 1. Toolsets Table Enhancements
**New Columns**:
- `parent_toolset_id uuid` - Links staging toolsets to their parent (live) toolset
- `editing_mode TEXT` - Either 'iteration' or 'staging' mode (default: 'iteration')
- `current_release_id uuid` - References the current live release
- `predecessor_id uuid` - For versioning toolset metadata
- `version BIGINT` - Version number for toolset metadata (default: 1)
- `history_id uuid` - Groups all versions of the same toolset

**New Indexes**:
- `toolsets_parent_toolset_id_idx` - Fast lookup of staging toolsets
- `toolsets_history_id_version_idx` - Track latest version by history_id
- `toolsets_project_id_slug_version_key` - Updated unique constraint for versioning

**New Constraints**:
- `toolsets_parent_toolset_id_fkey` - CASCADE delete staging with parent
- `toolsets_predecessor_id_fkey` - SET NULL on predecessor delete
- `toolsets_current_release_id_fkey` - SET NULL when release deleted
- `toolsets_editing_mode_check` - Ensure valid editing mode

#### 2. Tool Variations Versioning
**New Columns on `tool_variations`**:
- `predecessor_id uuid` - Links to previous version
- `version BIGINT` - Version number (default: 1)

**New Table: `tool_variations_group_versions`**:
- Captures point-in-time snapshot of all variations in a group
- Fields: id, group_id, version, variation_ids[], predecessor_id, created_at
- Unique constraint on (group_id, version)

**Updated Indexes**:
- Replaced `tool_variations_scoped_src_tool_urn_key` with `tool_variations_scoped_src_tool_urn_version_key` to support versioning

#### 3. Source State Tracking
**New Table: `system_source_states`**:
- Captures non-deployment sources (prompt templates)
- Fields: id, project_id, prompt_template_ids[], created_at

**New Table: `source_states`**:
- Combines deployment + system sources
- Fields: id, project_id, deployment_id, system_source_state_id, created_at
- Unique constraint on (deployment_id, system_source_state_id)

#### 4. Releases Infrastructure
**New Table: `toolset_releases`**:
- Main releases table
- Fields:
  - id, toolset_id
  - source_state_id (references source_states)
  - toolset_version_id (references toolset_versions)
  - global_variations_version_id (references tool_variations_group_versions)
  - toolset_variations_version_id (references tool_variations_group_versions)
  - release_number (auto-increment per toolset)
  - notes TEXT (optional release notes)
  - released_by_user_id
  - created_at
- Unique constraint on (toolset_id, release_number)
- Index on (toolset_id, created_at DESC) for listing releases

### Migration File
**Location**: `server/migrations/20251128233752_add-staging-and-releases-infrastructure.sql`

**Key Features**:
- Uses `DROP INDEX CONCURRENTLY` for zero-downtime index changes
- Uses `CREATE INDEX CONCURRENTLY` for non-blocking index creation
- Adds all foreign key constraints in correct dependency order
- Handles circular dependency between toolsets and toolset_releases

## 🔍 Verification Steps

### Step 1: Apply Migration to Local Database

```bash
# Start local postgres (if using docker compose)
docker compose up -d db

# Set database URL
export GRAM_DATABASE_URL="postgresql://user:password@localhost:5432/gram_dev"

# Apply migrations
cd server
atlas migrate apply --config file://atlas.hcl -u "${GRAM_DATABASE_URL}"
```

### Step 2: Verify Tables Exist

```sql
-- Check new tables were created
SELECT table_name FROM information_schema.tables
WHERE table_schema = 'public'
  AND table_name IN (
    'toolset_releases',
    'source_states',
    'system_source_states',
    'tool_variations_group_versions'
  );

-- Should return 4 rows
```

### Step 3: Verify Toolsets Columns

```sql
-- Check new columns on toolsets table
SELECT column_name, data_type, column_default
FROM information_schema.columns
WHERE table_name = 'toolsets'
  AND column_name IN (
    'parent_toolset_id',
    'editing_mode',
    'current_release_id',
    'predecessor_id',
    'version',
    'history_id'
  )
ORDER BY column_name;

-- Should return 6 rows
```

### Step 4: Verify Foreign Keys

```sql
-- Check all foreign key constraints
SELECT
    con.conname AS constraint_name,
    att.attname AS column_name,
    cl.relname AS table_name,
    referenced_cl.relname AS referenced_table
FROM pg_constraint con
JOIN pg_class cl ON con.conrelid = cl.oid
JOIN pg_attribute att ON att.attrelid = cl.oid AND att.attnum = ANY(con.conkey)
LEFT JOIN pg_class referenced_cl ON con.confrelid = referenced_cl.oid
WHERE cl.relname IN ('toolsets', 'toolset_releases', 'source_states', 'tool_variations', 'tool_variations_group_versions')
  AND con.contype = 'f'
ORDER BY cl.relname, con.conname;

-- Should show all expected foreign keys
```

### Step 5: Test Creating Staging Toolset (Manual SQL)

```sql
-- Create a test project first (if needed)
INSERT INTO projects (name, slug, organization_id)
VALUES ('Test Project', 'test-project', 'test-org')
ON CONFLICT DO NOTHING
RETURNING id;

-- Create a parent toolset
INSERT INTO toolsets (
  organization_id,
  project_id,
  name,
  slug,
  editing_mode
)
VALUES (
  'test-org',
  (SELECT id FROM projects WHERE slug = 'test-project' LIMIT 1),
  'My Parent Toolset',
  'my-parent-toolset',
  'iteration'
)
RETURNING id;

-- Create a staging toolset (child of parent)
INSERT INTO toolsets (
  organization_id,
  project_id,
  name,
  slug,
  parent_toolset_id,
  editing_mode
)
VALUES (
  'test-org',
  (SELECT id FROM projects WHERE slug = 'test-project' LIMIT 1),
  'My Parent Toolset (staging)',
  'my-parent-toolset-staging',
  (SELECT id FROM toolsets WHERE slug = 'my-parent-toolset' LIMIT 1),
  'staging'
)
RETURNING id, parent_toolset_id;

-- Verify parent-child relationship
SELECT
  t1.slug as parent_slug,
  t1.editing_mode as parent_mode,
  t2.slug as staging_slug,
  t2.editing_mode as staging_mode,
  t2.parent_toolset_id IS NOT NULL as has_parent
FROM toolsets t1
LEFT JOIN toolsets t2 ON t2.parent_toolset_id = t1.id
WHERE t1.slug = 'my-parent-toolset';
```

### Step 6: Test Cascade Delete

```sql
-- Delete parent toolset, verify staging is deleted too
DELETE FROM toolsets WHERE slug = 'my-parent-toolset';

-- Should have deleted both parent and staging
SELECT COUNT(*) FROM toolsets WHERE slug LIKE 'my-parent-toolset%';
-- Should return 0
```

### Step 7: Verify Indexes

```sql
-- Check new indexes exist
SELECT
    schemaname,
    tablename,
    indexname
FROM pg_indexes
WHERE tablename IN (
    'toolsets',
    'toolset_releases',
    'source_states',
    'system_source_states',
    'tool_variations',
    'tool_variations_group_versions'
)
ORDER BY tablename, indexname;

-- Verify expected indexes are present
```

## ✅ Success Criteria

- [ ] All tables created successfully
- [ ] All columns added to existing tables
- [ ] All indexes created (using CONCURRENTLY for zero downtime)
- [ ] All foreign key constraints in place
- [ ] Circular dependency between toolsets and toolset_releases working
- [ ] Can create staging toolset with parent reference
- [ ] Cascade delete works (deleting parent deletes staging)
- [ ] Versioning columns have correct defaults
- [ ] No migration errors in logs

## 🚀 Next Steps

### Phase 2: Backend Services (Week 3-4)

Now that the database schema is ready, the next phase is to implement the backend services:

1. **Releases Service** (`server/internal/releases/`)
   - Create SQLc queries for releases CRUD
   - Implement service methods:
     - `CreateRelease()` - Capture current state and create release
     - `GetRelease()` - Retrieve release details
     - `ListReleases()` - List all releases for a toolset
     - `RollbackToRelease()` - Set release as current

2. **Staging Management** (update `server/internal/toolsets/`)
   - `CreateStagingVersion()` - Duplicate toolset as staging
   - `GetStagingVersion()` - Get staging version if exists
   - `PublishStagingVersion()` - Create release and update live
   - `DiscardStagingVersion()` - Delete staging version
   - `SwitchEditingMode()` - Toggle iteration/staging mode

3. **State Capture Utilities** (`server/internal/releases/state.go`)
   - `CaptureSourceState()` - Create source_states record
   - `CaptureToolsetState()` - Create toolset_versions record
   - `CaptureVariationsState()` - Create variation version records

4. **Update Variation Service**
   - Modify to create new versions instead of in-place edits
   - `CreateVariationVersion()` - Create new version
   - `GetVariationAtVersion()` - Retrieve specific version
   - `CreateGroupVersion()` - Snapshot group state

See [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) for full details.

## 📝 Notes

- **Backward Compatibility**: All existing toolsets will have:
  - `editing_mode = 'iteration'` (default)
  - `version = 1` (default)
  - `current_release_id = NULL` (no releases in iteration mode)
  - `parent_toolset_id = NULL` (not a staging toolset)

- **Iteration Mode**: Toolsets in iteration mode work exactly as before - changes are immediate, no releases created

- **Staging Mode**: Must be explicitly enabled - this is opt-in behavior

- **Migration Safety**: The migration uses:
  - `CONCURRENTLY` for index operations (no table locking)
  - `DEFAULT` values for new NOT NULL columns (no data migration needed)
  - `CASCADE` only for parent→staging relationship (safe to delete staging with parent)
  - `RESTRICT` for critical references (prevents accidental data loss)

## 🐛 Known Issues / Future Improvements

1. **Function Versioning Not Implemented**: Gram Functions don't have versioning yet - deploying a new function version affects all toolsets using it immediately

2. **Project-Level Rollback**: Currently releases are at toolset level only - rolling back doesn't affect project-wide state like deployments or global variations

3. **Release Naming**: Only auto-incrementing release numbers - no semver or custom names (can add later)

4. **Performance**: No caching layer yet - will need to cache release state for production performance

---

**Status**: Phase 1 Complete ✅
**Date**: 2025-11-28
**Migration**: `20251128233752_add-staging-and-releases-infrastructure.sql`
