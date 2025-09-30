-- name: GetMetadataForToolset :one
SELECT id,
       toolset_id,
       external_documentation_url,
       logo_id,
       created_at,
       updated_at
FROM mcp_install_page_metadata
WHERE toolset_id = $1
ORDER BY updated_at DESC
LIMIT 1;

-- name: CreateMetadata :one
INSERT INTO mcp_install_page_metadata (toolset_id, external_documentation_url, logo_id)
VALUES ($1, $2, $3)
RETURNING id,
          toolset_id,
          external_documentation_url,
          logo_id,
          created_at,
          updated_at;

-- name: UpdateMetadata :one
UPDATE mcp_install_page_metadata
SET toolset_id = $2,
    external_documentation_url = $3,
    logo_id = $4,
    updated_at = clock_timestamp()
WHERE id = $1
RETURNING id,
          toolset_id,
          external_documentation_url,
          logo_id,
          created_at,
          updated_at;

-- name: EnsureToolsetOwnership :one
SELECT id
FROM toolsets
WHERE id = $1
  AND project_id = $2
  AND deleted IS FALSE;
