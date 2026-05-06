-- name: LockAssistantForMemoryWrite :exec
-- Acquire a transaction-scoped advisory lock keyed on the assistant ID so
-- Remember/Forget mutations on the same assistant serialize across processes.
SELECT pg_advisory_xact_lock(hashtextextended(@assistant_id::text, 0));

-- name: InsertAssistantMemory :one
INSERT INTO assistant_memories (
  assistant_id,
  project_id,
  organization_id,
  content,
  embedding,
  tags,
  origin_thread_id,
  origin_chat_id,
  supersedes_id
) VALUES (
  @assistant_id::uuid,
  @project_id::uuid,
  @organization_id,
  @content,
  @embedding,
  @tags,
  @origin_thread_id,
  @origin_chat_id,
  @supersedes_id
)
RETURNING id, created_at;

-- name: MarkAssistantMemorySuperseded :exec
UPDATE assistant_memories
   SET superseded_at = clock_timestamp(),
       updated_at    = clock_timestamp()
 WHERE id = @id
   AND superseded_at IS NULL
   AND deleted_at    IS NULL;

-- name: SoftDeleteAssistantMemory :one
-- Returns the deleted row's audit-relevant fields so callers do not need a
-- separate SELECT for the audit log entry.
UPDATE assistant_memories
   SET deleted_at = clock_timestamp(),
       updated_at = clock_timestamp()
 WHERE id = @id
   AND deleted_at IS NULL
RETURNING id, organization_id, project_id::uuid AS project_id, assistant_id::uuid AS assistant_id;

-- name: SoftDeleteAssistantMemoryByProject :one
-- Project-scoped variant for the management API: filters on (id, project_id)
-- so a delete cannot escape the caller's project boundary.
UPDATE assistant_memories
   SET deleted_at = clock_timestamp(),
       updated_at = clock_timestamp()
 WHERE id = @id
   AND project_id = @project_id::uuid
   AND deleted_at IS NULL
RETURNING id, organization_id, project_id::uuid AS project_id, assistant_id::uuid AS assistant_id;

-- name: CountActiveAssistantMemories :one
SELECT count(*) AS active_count
  FROM assistant_memories
 WHERE assistant_id = @assistant_id::uuid
   AND deleted_at IS NULL
   AND superseded_at IS NULL;

-- name: GetNearestActiveAssistantMemory :one
-- Top-1 nearest active memory for an assistant, used by Remember to dedup
-- against the closest existing memory regardless of tag.
SELECT
    id,
    content,
    (1 - (embedding <=> @query_embedding))::float8 AS similarity
  FROM assistant_memories
 WHERE assistant_id = @assistant_id::uuid
   AND deleted_at IS NULL
   AND superseded_at IS NULL
 ORDER BY embedding <=> @query_embedding
 LIMIT 1;

-- name: ListNearestAssistantMemories :many
-- Top-K nearest active memories with an optional tag overlap filter. Caller
-- passes an empty array to skip the tag filter; otherwise rows must overlap.
SELECT
    id,
    content,
    tags,
    created_at,
    last_access,
    (1 - (embedding <=> @query_embedding))::float8 AS similarity
  FROM assistant_memories
 WHERE assistant_id = @assistant_id::uuid
   AND deleted_at IS NULL
   AND superseded_at IS NULL
   AND (cardinality(@tags::text[]) = 0 OR tags && @tags::text[])
 ORDER BY embedding <=> @query_embedding
 LIMIT @result_limit;

-- name: BumpAssistantMemoryLastAccess :exec
UPDATE assistant_memories
   SET last_access = clock_timestamp()
 WHERE id = ANY(@ids::uuid[]);

-- name: GetAssistantMemoryByID :one
-- Fetches a row by id scoped to a project, without a soft-delete filter so
-- the management API can expose deleted rows when include_deleted=true;
-- callers filter in Go. Embedding column is omitted to keep payloads small.
SELECT
    id,
    assistant_id::uuid AS assistant_id,
    project_id::uuid AS project_id,
    organization_id,
    content,
    supersedes_id,
    superseded_at,
    valid_at,
    tags,
    origin_thread_id,
    origin_chat_id,
    created_at,
    updated_at,
    last_access,
    deleted_at
  FROM assistant_memories
 WHERE id = @id
   AND project_id = @project_id::uuid;

-- name: ListAssistantMemoriesForAdmin :many
-- Management API listing for an assistant scoped to its project. Returns
-- superseded rows because they are part of the audit trail. Filters:
--   - tags overlap (skipped when an empty array is passed)
--   - include_deleted=false restricts to live rows
-- Cursor pagination is keyset on (created_at DESC, id DESC).
-- Embedding column is omitted to keep payloads small.
SELECT
    id,
    assistant_id::uuid AS assistant_id,
    project_id::uuid AS project_id,
    organization_id,
    content,
    supersedes_id,
    superseded_at,
    valid_at,
    tags,
    origin_thread_id,
    origin_chat_id,
    created_at,
    updated_at,
    last_access,
    deleted_at
  FROM assistant_memories
 WHERE assistant_id = @assistant_id::uuid
   AND project_id = @project_id::uuid
   AND (cardinality(@tags::text[]) = 0 OR tags && @tags::text[])
   AND (@include_deleted::bool OR deleted_at IS NULL)
   AND (
     sqlc.narg(cursor_created_at)::timestamptz IS NULL
     OR created_at < sqlc.narg(cursor_created_at)::timestamptz
     OR (
       created_at = sqlc.narg(cursor_created_at)::timestamptz
       AND id < sqlc.narg(cursor_id)::uuid
     )
   )
 ORDER BY created_at DESC, id DESC
 LIMIT @page_limit;

-- name: ReapSoftDeletedAssistantMemoriesOlderThan :execrows
-- Hard-deletes all rows whose deleted_at is older than the cutoff, plus every
-- predecessor reachable via the supersedes_id chain. The recursive CTE walks
-- backwards from each soft-deleted head so historical superseded rows that
-- only existed to support the deleted head also drop on the same schedule.
WITH RECURSIVE chain AS (
  SELECT am.id
    FROM assistant_memories am
   WHERE am.deleted_at IS NOT NULL
     AND am.deleted_at < @cutoff
  UNION ALL
  SELECT m.id
    FROM assistant_memories m
    JOIN chain c ON m.supersedes_id = c.id
)
DELETE FROM assistant_memories
 WHERE id IN (SELECT id FROM chain);
