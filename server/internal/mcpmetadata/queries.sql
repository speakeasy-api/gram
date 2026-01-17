-- name: GetMetadataForToolset :one
SELECT id,
       toolset_id,
       project_id,
       external_documentation_url,
       logo_id,
       instructions,
       user_agent,
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
    user_agent
) VALUES (@toolset_id, @project_id, @external_documentation_url, @logo_id, @instructions, @user_agent)
ON CONFLICT (toolset_id)
DO UPDATE SET project_id = EXCLUDED.project_id,
              external_documentation_url = EXCLUDED.external_documentation_url,
              logo_id = EXCLUDED.logo_id,
              instructions = EXCLUDED.instructions,
              user_agent = EXCLUDED.user_agent,
              updated_at = clock_timestamp()
RETURNING id,
          toolset_id,
          project_id,
          external_documentation_url,
          logo_id,
          instructions,
          user_agent,
          created_at,
          updated_at;
