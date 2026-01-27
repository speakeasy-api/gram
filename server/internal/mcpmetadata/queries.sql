-- name: GetMetadataForToolset :one
SELECT *
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
    instructions
) VALUES (@toolset_id, @project_id, @external_documentation_url, @logo_id, @instructions)
ON CONFLICT (toolset_id)
DO UPDATE SET project_id = EXCLUDED.project_id,
              external_documentation_url = EXCLUDED.external_documentation_url,
              logo_id = EXCLUDED.logo_id,
              instructions = EXCLUDED.instructions,
              updated_at = clock_timestamp()
RETURNING *;

-- name: UpdateHeaderDisplayName :one
-- Updates a single header display name in the JSONB field.
-- If display_name is empty, removes the key from the map.
UPDATE mcp_metadata
SET header_display_names = CASE
    WHEN @display_name::TEXT = '' THEN header_display_names - @security_key::TEXT
    ELSE jsonb_set(header_display_names, ARRAY[@security_key::TEXT], to_jsonb(@display_name::TEXT))
    END,
    updated_at = clock_timestamp()
WHERE toolset_id = @toolset_id AND project_id = @project_id
RETURNING id,
          toolset_id,
          project_id,
          external_documentation_url,
          logo_id,
          instructions,
          header_display_names,
          created_at,
          updated_at;

-- name: GetHeaderDisplayNames :one
SELECT header_display_names
FROM mcp_metadata
WHERE toolset_id = @toolset_id;
