-- name: GetMetadataByBackendID :one
-- A canonical wrapper shares its toolset's id, so one id matches the row under either key.
SELECT id,
       toolset_id,
       mcp_server_id,
       project_id,
       external_documentation_url,
       external_documentation_text,
       logo_id,
       instructions,
       header_display_names,
       default_environment_id,
       installation_override_url,
       created_at,
       updated_at
FROM mcp_metadata
WHERE project_id = @project_id
  AND (toolset_id = @backend_id::uuid OR mcp_server_id = @backend_id::uuid)
ORDER BY (toolset_id IS NOT NULL) DESC, updated_at DESC
LIMIT 1;

-- name: UpsertMetadata :one
INSERT INTO mcp_metadata (
    toolset_id,
    project_id,
    external_documentation_url,
    external_documentation_text,
    logo_id,
    instructions,
    default_environment_id,
    installation_override_url
) VALUES (@toolset_id, @project_id, @external_documentation_url, @external_documentation_text, @logo_id, @instructions, @default_environment_id, @installation_override_url)
ON CONFLICT (toolset_id) WHERE toolset_id IS NOT NULL
DO UPDATE SET project_id = EXCLUDED.project_id,
              external_documentation_url = EXCLUDED.external_documentation_url,
              external_documentation_text = EXCLUDED.external_documentation_text,
              logo_id = EXCLUDED.logo_id,
              instructions = EXCLUDED.instructions,
              default_environment_id = EXCLUDED.default_environment_id,
              installation_override_url = EXCLUDED.installation_override_url,
              updated_at = clock_timestamp()
RETURNING id,
          toolset_id,
          mcp_server_id,
          project_id,
          external_documentation_url,
          external_documentation_text,
          logo_id,
          instructions,
          header_display_names,
          default_environment_id,
          installation_override_url,
          created_at,
          updated_at;

-- name: UpsertMetadataByMcpServerID :one
INSERT INTO mcp_metadata (
    mcp_server_id,
    project_id,
    external_documentation_url,
    external_documentation_text,
    logo_id,
    instructions,
    default_environment_id,
    installation_override_url
) VALUES (@mcp_server_id, @project_id, @external_documentation_url, @external_documentation_text, @logo_id, @instructions, @default_environment_id, @installation_override_url)
ON CONFLICT (mcp_server_id) WHERE mcp_server_id IS NOT NULL
DO UPDATE SET project_id = EXCLUDED.project_id,
              external_documentation_url = EXCLUDED.external_documentation_url,
              external_documentation_text = EXCLUDED.external_documentation_text,
              logo_id = EXCLUDED.logo_id,
              instructions = EXCLUDED.instructions,
              default_environment_id = EXCLUDED.default_environment_id,
              installation_override_url = EXCLUDED.installation_override_url,
              updated_at = clock_timestamp()
RETURNING id,
          toolset_id,
          mcp_server_id,
          project_id,
          external_documentation_url,
          external_documentation_text,
          logo_id,
          instructions,
          header_display_names,
          default_environment_id,
          installation_override_url,
          created_at,
          updated_at;

-- name: SetDefaultLogoIfUnset :one
-- Used for system-assigned defaults (e.g. an unproxied server's favicon)
-- rather than a user-initiated edit: only ever touches logo_id, and only
-- when unset, so it can never race a concurrent user edit into clobbering
-- their branding/install-page settings the way a full-record upsert would.
INSERT INTO mcp_metadata (
    mcp_server_id,
    project_id,
    logo_id
) VALUES (@mcp_server_id, @project_id, @logo_id)
ON CONFLICT (mcp_server_id) WHERE mcp_server_id IS NOT NULL
DO UPDATE SET logo_id = EXCLUDED.logo_id,
              updated_at = clock_timestamp()
WHERE mcp_metadata.logo_id IS NULL
RETURNING id,
          toolset_id,
          mcp_server_id,
          project_id,
          external_documentation_url,
          external_documentation_text,
          logo_id,
          instructions,
          header_display_names,
          default_environment_id,
          installation_override_url,
          created_at,
          updated_at;

-- name: SetHeaderDisplayNames :one
UPDATE mcp_metadata
SET header_display_names = @header_display_names
WHERE id = @id
  AND project_id = @project_id
RETURNING id,
          toolset_id,
          mcp_server_id,
          project_id,
          external_documentation_url,
          external_documentation_text,
          logo_id,
          instructions,
          header_display_names,
          default_environment_id,
          installation_override_url,
          created_at,
          updated_at;

-- name: LockMetadataByBackendID :many
-- Locks the toolset-keyed row before its server-keyed twin, the order the dependent move takes.
SELECT id,
       toolset_id,
       mcp_server_id,
       project_id,
       external_documentation_url,
       external_documentation_text,
       logo_id,
       instructions,
       header_display_names,
       default_environment_id,
       installation_override_url,
       created_at,
       updated_at
FROM mcp_metadata
WHERE project_id = @project_id
  AND (toolset_id = @backend_id::uuid OR mcp_server_id = @backend_id::uuid)
ORDER BY (toolset_id IS NOT NULL) DESC, updated_at DESC
FOR UPDATE;

-- name: AcquireMetadataBackendLock :exec
-- Serializes saves for one backend so a save sees a row another save inserted under the other key.
SELECT pg_advisory_xact_lock(hashtextextended('mcp_metadata:' || (@project_id::uuid)::text || ':' || (@backend_id::uuid)::text, 0));

-- name: DeleteServerKeyedMetadata :execrows
-- Cascades to the row's mcp_environment_configs.
DELETE FROM mcp_metadata
WHERE id = @id
  AND project_id = @project_id
  AND mcp_server_id = @mcp_server_id;

-- name: RekeyMetadataToMcpServer :execrows
-- Same row id, so its mcp_environment_configs stay attached.
UPDATE mcp_metadata
SET toolset_id = NULL,
    mcp_server_id = @mcp_server_id,
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND toolset_id = @mcp_server_id;

-- name: GetHeaderDisplayNames :one
SELECT header_display_names
FROM mcp_metadata
WHERE project_id = @project_id
  AND (toolset_id = @backend_id::uuid OR mcp_server_id = @backend_id::uuid)
ORDER BY (toolset_id IS NOT NULL) DESC, updated_at DESC
LIMIT 1;

-- name: GetHeaderDisplayNamesByToolsetIDs :many
-- Toolset-keyed rows sort last so they win when the caller builds a map.
SELECT COALESCE(toolset_id, mcp_server_id)::uuid AS toolset_id, header_display_names
FROM mcp_metadata
WHERE project_id = @project_id
  AND (toolset_id = ANY(@toolset_ids::uuid[]) OR mcp_server_id = ANY(@toolset_ids::uuid[]))
ORDER BY (toolset_id IS NOT NULL), updated_at;

-- name: GetHeaderDisplayNamesByMcpServerID :one
SELECT header_display_names
FROM mcp_metadata
WHERE mcp_server_id = @mcp_server_id;

-- name: ListEnvironmentConfigs :many
SELECT id,
       project_id,
       mcp_metadata_id,
       variable_name,
       header_display_name,
       provided_by,
       created_at,
       updated_at
FROM mcp_environment_configs
WHERE mcp_metadata_id = @mcp_metadata_id
ORDER BY variable_name ASC;

-- name: UpsertEnvironmentConfig :one
INSERT INTO mcp_environment_configs (
    project_id,
    mcp_metadata_id,
    variable_name,
    header_display_name,
    provided_by
) VALUES (@project_id, @mcp_metadata_id, @variable_name, @header_display_name, @provided_by)
ON CONFLICT (mcp_metadata_id, variable_name)
DO UPDATE SET header_display_name = EXCLUDED.header_display_name,
              provided_by = EXCLUDED.provided_by,
              updated_at = clock_timestamp()
RETURNING id,
          project_id,
          mcp_metadata_id,
          variable_name,
          header_display_name,
          provided_by,
          created_at,
          updated_at;

-- name: DeleteEnvironmentConfig :exec
DELETE FROM mcp_environment_configs
WHERE mcp_metadata_id = @mcp_metadata_id
  AND variable_name = @variable_name;

-- name: DeleteAllEnvironmentConfigs :exec
DELETE FROM mcp_environment_configs
WHERE mcp_metadata_id = @mcp_metadata_id;
