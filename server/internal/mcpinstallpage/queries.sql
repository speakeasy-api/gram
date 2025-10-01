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

-- name: UpsertMetadata :one
INSERT INTO mcp_install_page_metadata (toolset_id, external_documentation_url, logo_id)
VALUES ($1, $2, $3)
ON CONFLICT (toolset_id)
DO UPDATE SET external_documentation_url = EXCLUDED.external_documentation_url,
              logo_id = EXCLUDED.logo_id,
              updated_at = clock_timestamp()
RETURNING id,
          toolset_id,
          external_documentation_url,
          logo_id,
          created_at,
          updated_at;

