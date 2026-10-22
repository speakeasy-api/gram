-- name: GetMetadataByBackendID :one
-- Addressed by toolset id, the row counts only under the toolset's canonical wrapper, which shares its id.
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
WHERE mcp_metadata.project_id = @project_id::uuid
  AND mcp_metadata.mcp_server_id = @backend_id::uuid
  AND (NOT @toolset_backend::bool OR EXISTS (SELECT 1 FROM mcp_servers s WHERE s.id = @backend_id::uuid AND s.toolset_id = s.id AND s.project_id = @project_id::uuid));

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

-- name: LockMetadataByBackendID :one
-- The same match as GetMetadataByBackendID.
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
WHERE mcp_metadata.project_id = @project_id::uuid
  AND mcp_metadata.mcp_server_id = @backend_id::uuid
  AND (NOT @toolset_backend::bool OR EXISTS (SELECT 1 FROM mcp_servers s WHERE s.id = @backend_id::uuid AND s.toolset_id = s.id AND s.project_id = @project_id::uuid))
FOR UPDATE;

-- name: AcquireMetadataBackendLock :exec
-- Serializes saves for one backend id, so a save sees any row a concurrent save inserted for it.
SELECT pg_advisory_xact_lock(hashtextextended('mcp_metadata:' || (@project_id::uuid)::text || ':' || (@backend_id::uuid)::text, 0));

-- name: GetHeaderDisplayNames :one
-- Addressed by toolset id: the row counts only under the toolset's canonical wrapper.
SELECT header_display_names
FROM mcp_metadata
WHERE mcp_metadata.project_id = @project_id::uuid
  AND mcp_metadata.mcp_server_id = @backend_id::uuid
  AND EXISTS (SELECT 1 FROM mcp_servers s WHERE s.id = @backend_id::uuid AND s.toolset_id = s.id AND s.project_id = @project_id::uuid);

-- name: GetHeaderDisplayNamesByToolsetIDs :many
-- A row counts only under the toolset's canonical wrapper, which shares its id.
SELECT m.mcp_server_id::uuid AS toolset_id, m.header_display_names
FROM mcp_metadata m
WHERE m.project_id = @project_id
  AND m.mcp_server_id = ANY(@toolset_ids::uuid[])
  AND EXISTS (SELECT 1 FROM mcp_servers s WHERE s.id = m.mcp_server_id AND s.toolset_id = s.id AND s.project_id = m.project_id);

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
