# Async Name Mapper with ClickHouse Backfill

## Current State

- Name mapper synchronously calls LLM to map tool sources (e.g., UUID → "Linear")
- Mapping stored in Redis cache with 1yr TTL
- Called during hook processing in `pending_helpers.go:126-134`
- No ClickHouse backfill when mapping discovered

## Plan

### 1. Create Temporal Activity: GenerateNameMapping

**File**: `server/internal/background/activities/generate_name_mapping.go`

- Input: `GenerateNameMappingArgs{ServerName, ToolCallAttrs, OrgID, ProjectID}`
- Call LLM via `openrouter.CompletionClient` (reuse existing prompt from `name_mapper.go:83-86`)
- Return `GenerateNameMappingResult{OriginalName, MappedName}`
- Store mapping in Redis cache (reuse `nameMappingKey` pattern)
- Retries: 3 attempts, exponential backoff

### 2. Create Temporal Activity: UpdateClickHouseToolSource

**File**: `server/internal/background/activities/update_clickhouse_tool_source.go`
**New method in telemetry service**: `UpdateToolSourceBulk(ctx, projectID, oldSource, newSource)`

ClickHouse update strategy:

```sql
ALTER TABLE telemetry_logs
UPDATE attributes = JSONSet(attributes, 'gram.tool_call.source', ?)
WHERE project_id = ?
  AND toString(attributes.gram.tool_call.source) = ?
```

**CRITICAL**: ClickHouse mutations are **async** and **non-transactional**

- Use `ALTER TABLE...UPDATE` (mutation)
- Won't update materialized columns directly - those derive from attributes JSON
- Materialize view `trace_summaries_mv` will update on next aggregation
- Input: `UpdateClickHouseToolSourceArgs{ProjectID, OldSource, NewSource}`
- Execute mutation, don't wait for completion (ClickHouse async nature)
- Retries: 5 attempts (mutations may be slow/resource constrained)

### 3. Create Workflow: ProcessNameMappingWorkflow

**File**: `server/internal/background/process_name_mapping.go`

- `ProcessNameMappingWorkflowParams{ServerName, OrgID, ProjectID, ToolCallAttrs}`
- Workflow ID: `v1:process-name-mapping:{serverName}:{projectID}`
- ID reuse policy: `ALLOW_DUPLICATE_FAILED_ONLY` (don't rerun if succeeded)
- Timeout: 5 minutes
- Orchestration:
  1. Execute `GenerateNameMapping` activity
  2. If mapping found (non-empty):
     - Execute `UpdateClickHouseToolSource` activity in parallel for each project/org that uses this source
     - Return success
  3. If no mapping: return nil (not an error)

### 4. Update Hooks Service Integration

**File**: `server/internal/hooks/pending_helpers.go`

- Remove synchronous `s.nameMapper.GetMappedName()` call (lines 126-134)
- Instead: check Redis cache synchronously
  - Cache hit: use cached mapping immediately
  - Cache miss: trigger workflow asynchronously, continue without mapping
- Add `temporalClient` dependency to hooks `Service` struct
- Call `ExecuteProcessNameMappingWorkflow` with fire-and-forget pattern

**File**: `server/internal/hooks/impl.go`

- Update `NewService` to accept `temporalClient client.Client`
- Store in Service struct

**File**: `server/cmd/gram/start.go`

- Pass temporal client when constructing hooks service

### 5. Update Background Activities Registration

**File**: `server/internal/background/activities.go`

- Add activity fields to `Activities` struct
- Wire up in `NewActivities` constructor

**File**: `server/cmd/gram/worker.go`

- Register `ProcessNameMappingWorkflow`
- Register activities

### 6. Testing

**File**: `server/internal/background/activities/generate_name_mapping_test.go`

- Test successful mapping generation
- Test empty mapping (LLM returns empty string)
- Test LLM client error handling

**File**: `server/internal/background/activities/update_clickhouse_tool_source_test.go`

- Insert test logs with `tool_source` = "old-source"
- Run activity to update to "new-source"
- Sleep 500ms (ClickHouse mutation + eventual consistency)
- Query logs, verify attributes JSON updated
- Verify materialized `tool_source` column reflects change (via re-materialization)

**File**: `server/internal/background/process_name_mapping_test.go`

- Test full workflow end-to-end with testenv infra
- Verify Redis cache populated
- Verify ClickHouse records updated

## Skills Needed

- `golang` (active)
- `clickhouse` (active)

## Unresolved Questions

1. **ClickHouse mutation scope**: Should we update ALL projects/orgs using this source, or just the current project?
   - **Recommendation**: Just current project for safety and cost
   - Reasoning: Source names might collide across orgs (e.g., one org's "uuid-123" is Linear, another's is Notion)

2. **Workflow idempotency**: What if same source appears in multiple hooks rapidly?
   - Current plan uses `ALLOW_DUPLICATE_FAILED_ONLY` + workflow ID based on `serverName:projectID`
   - This means only ONE workflow per source+project combination runs at a time
   - **Question**: Is this the right granularity?

3. **Redis cache consistency**: If workflow fails after LLM succeeds but before ClickHouse update, cache has mapping but DB doesn't
   - **Recommendation**: Acceptable - cache will be used going forward, old records less critical
   - Alternative: Don't cache until after ClickHouse succeeds (adds complexity)

4. **Performance**: Should we batch multiple source mappings in one workflow?
   - Current plan: one workflow per source
   - Alternative: batch workflow that processes multiple sources (more complex)
   - **Question**: What's the expected volume of new sources?
