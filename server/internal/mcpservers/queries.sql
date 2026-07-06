-- name: CreateMCPServer :one
INSERT INTO mcp_servers (
    id,
    project_id,
    name,
    slug,
    environment_id,
    user_session_issuer_id,
    remote_mcp_server_id,
    tunneled_mcp_server_id,
    toolset_id,
    tool_variations_group_id,
    visibility
)
VALUES (
    @id,
    @project_id,
    @name,
    @slug,
    @environment_id,
    @user_session_issuer_id,
    @remote_mcp_server_id,
    @tunneled_mcp_server_id,
    @toolset_id,
    @tool_variations_group_id,
    @visibility
)
RETURNING *;

-- name: GetMCPServerByIDAndProjectID :one
SELECT *
FROM mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetMCPServerByIDAndOrganizationID :one
-- Fetch an MCP server by id scoped to an organization via its project's
-- organization_id. For organization-administrator flows that span projects but
-- must stay within the caller's org (e.g. remote session client detach).
SELECT m.*
FROM mcp_servers AS m
JOIN projects AS p ON p.id = m.project_id
WHERE m.id = @id
  AND p.organization_id = @organization_id
  AND m.deleted IS FALSE;

-- name: GetMCPServerBySlug :one
SELECT *
FROM mcp_servers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListMCPServersByProjectID :many
SELECT *
FROM mcp_servers
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('remote_mcp_server_id')::uuid IS NULL OR remote_mcp_server_id = sqlc.narg('remote_mcp_server_id')::uuid)
  AND (sqlc.narg('tunneled_mcp_server_id')::uuid IS NULL OR tunneled_mcp_server_id = sqlc.narg('tunneled_mcp_server_id')::uuid)
  AND (sqlc.narg('toolset_id')::uuid IS NULL OR toolset_id = sqlc.narg('toolset_id')::uuid)
ORDER BY created_at DESC;

-- name: ListMCPServersByOrganizationID :many
-- List every MCP server in an organization via each project's organization_id.
-- For organization-administrator flows that span projects (e.g. the RBAC
-- connection-policy picker), which carry no project scope.
SELECT m.*
FROM mcp_servers AS m
JOIN projects AS p ON p.id = m.project_id
WHERE p.organization_id = @organization_id
  AND m.deleted IS FALSE
  AND p.deleted IS FALSE
ORDER BY m.created_at DESC;

-- name: ListMCPServersForTelemetryByProjectID :many
-- Includes soft-deleted servers so tool-usage telemetry can classify historical
-- rows whose backing MCP server has since been deleted (or recreated). The
-- backend source ids (remote_mcp_server_id / tunneled_mcp_server_id) recorded on
-- telemetry rows outlive the mcp_servers row, so matching against deleted rows
-- keeps a call's target_type stable instead of falling through to
-- shadow_mcp_server. Live servers are ordered first so a source id shared by a
-- live and a deleted server resolves to the live one.
SELECT id, name, slug, remote_mcp_server_id, tunneled_mcp_server_id
FROM mcp_servers
WHERE project_id = @project_id
ORDER BY deleted ASC, created_at DESC;

-- name: UpdateMCPServer :one
UPDATE mcp_servers
SET
    name = @name,
    slug = @slug,
    environment_id = @environment_id,
    user_session_issuer_id = COALESCE(sqlc.narg('user_session_issuer_id'), user_session_issuer_id),
    remote_mcp_server_id = @remote_mcp_server_id,
    tunneled_mcp_server_id = @tunneled_mcp_server_id,
    toolset_id = @toolset_id,
    tool_variations_group_id = @tool_variations_group_id,
    visibility = @visibility,
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteMCPServer :one
UPDATE mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: SetMCPServerToolMetadata :many
-- Authoritative write of an MCP server's tool metadata collection: every tool in
-- @tools is upserted and every stored tool absent from @tools is retired, in a
-- single statement. The two branches touch disjoint rows (partitioned by
-- tool_name), so neither observes the other's writes.
--
-- The conflict target is the partial unique index on (mcp_server_id, tool_name)
-- WHERE deleted IS FALSE, so re-adding a retired tool inserts a fresh row
-- rather than resurrecting the tombstoned one. A tool_name repeated within
-- @tools trips the ON CONFLICT cardinality check (SQLSTATE 21000), aborting the
-- statement rather than silently applying one of the duplicates.
-- The input CTE unpacks each element with ->> rather than jsonb_to_recordset:
-- sqlc's static analyzer never learns a column-definition list, so a
-- recordset alias leaves every input.<column> reference unresolvable (and
-- crashes sqlc outright under the managed analyzer). Selecting from
-- jsonb_array_elements references only the element itself, and the per-column
-- names below are ordinary select-list aliases, which sqlc does understand.
-- ->> yields NULL for both an absent key and a JSON null, which is exactly the
-- "hint unset" case these nullable columns encode.
WITH input AS (
    SELECT
        elem->>'tool_name' AS tool_name,
        elem->>'title' AS title,
        (elem->>'read_only_hint')::boolean AS read_only_hint,
        (elem->>'destructive_hint')::boolean AS destructive_hint,
        (elem->>'idempotent_hint')::boolean AS idempotent_hint,
        (elem->>'open_world_hint')::boolean AS open_world_hint
    FROM jsonb_array_elements(@tools::jsonb) AS elem
),
upserted AS (
    INSERT INTO mcp_server_tool_metadata (
        project_id,
        mcp_server_id,
        tool_name,
        title,
        read_only_hint,
        destructive_hint,
        idempotent_hint,
        open_world_hint
    )
    SELECT
        @project_id,
        @mcp_server_id,
        input.tool_name,
        input.title,
        input.read_only_hint,
        input.destructive_hint,
        input.idempotent_hint,
        input.open_world_hint
    FROM input
    ON CONFLICT (mcp_server_id, tool_name) WHERE deleted IS FALSE
    DO UPDATE SET
        title = EXCLUDED.title,
        read_only_hint = EXCLUDED.read_only_hint,
        destructive_hint = EXCLUDED.destructive_hint,
        idempotent_hint = EXCLUDED.idempotent_hint,
        open_world_hint = EXCLUDED.open_world_hint,
        updated_at = clock_timestamp()
    WHERE mcp_server_tool_metadata.project_id = @project_id
    RETURNING *
),
retired AS (
    UPDATE mcp_server_tool_metadata
    SET deleted_at = clock_timestamp()
    WHERE mcp_server_id = @mcp_server_id
      AND project_id = @project_id
      AND deleted IS FALSE
      AND NOT EXISTS (
          SELECT 1 FROM input WHERE input.tool_name = mcp_server_tool_metadata.tool_name
      )
    RETURNING *
)
SELECT
    id, project_id, mcp_server_id, tool_name, title,
    read_only_hint, destructive_hint, idempotent_hint, open_world_hint,
    created_at, updated_at, deleted_at, deleted,
    false AS was_retired
FROM upserted
UNION ALL
SELECT
    id, project_id, mcp_server_id, tool_name, title,
    read_only_hint, destructive_hint, idempotent_hint, open_world_hint,
    created_at, updated_at, deleted_at, deleted,
    true AS was_retired
FROM retired
ORDER BY tool_name;

-- name: ListMCPServerToolMetadata :many
SELECT *
FROM mcp_server_tool_metadata
WHERE mcp_server_id = @mcp_server_id
  AND project_id = @project_id
  AND (@include_deleted::boolean OR deleted IS FALSE)
ORDER BY tool_name, created_at;

-- name: UpdateMCPServerToolMetadata :one
UPDATE mcp_server_tool_metadata
SET title = @title,
    read_only_hint = @read_only_hint,
    destructive_hint = @destructive_hint,
    idempotent_hint = @idempotent_hint,
    open_world_hint = @open_world_hint,
    updated_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id
  AND project_id = @project_id
  AND tool_name = @tool_name
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteMCPServerToolMetadata :one
UPDATE mcp_server_tool_metadata
SET deleted_at = clock_timestamp()
WHERE mcp_server_id = @mcp_server_id
  AND project_id = @project_id
  AND tool_name = @tool_name
  AND deleted IS FALSE
RETURNING *;
