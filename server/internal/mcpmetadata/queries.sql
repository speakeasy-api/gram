-- name: GetMetadataForToolset :one
SELECT id,
       toolset_id,
       project_id,
       external_documentation_url,
       logo_id,
       instructions,
       header_display_names,
       default_environment_id,
       installation_override_url,
       created_at,
       updated_at
FROM mcp_metadata
WHERE toolset_id = @toolset_id
ORDER BY updated_at DESC
LIMIT 1;

-- name: UpsertMetadata :one
INSERT INTO mcp_metadata (
    toolset_id,
    project_id,
    external_documentation_url,
    logo_id,
    instructions,
    default_environment_id,
    installation_override_url
) VALUES (@toolset_id, @project_id, @external_documentation_url, @logo_id, @instructions, @default_environment_id, @installation_override_url)
ON CONFLICT (toolset_id)
DO UPDATE SET project_id = EXCLUDED.project_id,
              external_documentation_url = EXCLUDED.external_documentation_url,
              logo_id = EXCLUDED.logo_id,
              instructions = EXCLUDED.instructions,
              default_environment_id = EXCLUDED.default_environment_id,
              installation_override_url = EXCLUDED.installation_override_url,
              updated_at = clock_timestamp()
RETURNING id,
          toolset_id,
          project_id,
          external_documentation_url,
          logo_id,
          instructions,
          header_display_names,
          default_environment_id,
          installation_override_url,
          created_at,
          updated_at;

-- name: GetHeaderDisplayNames :one
SELECT header_display_names
FROM mcp_metadata
WHERE toolset_id = @toolset_id;

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
