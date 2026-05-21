-- name: GetPortalByProjectID :one
SELECT *
FROM project_portals
WHERE project_id = @project_id;

-- name: UpsertPortal :one
INSERT INTO project_portals (
    project_id,
    enabled,
    display_name,
    tagline,
    logo_asset_id
)
VALUES (
    @project_id,
    @enabled,
    @display_name,
    @tagline,
    @logo_asset_id
)
ON CONFLICT (project_id) DO UPDATE
SET
    enabled = EXCLUDED.enabled,
    display_name = EXCLUDED.display_name,
    tagline = EXCLUDED.tagline,
    logo_asset_id = EXCLUDED.logo_asset_id,
    updated_at = clock_timestamp()
RETURNING *;

-- name: ListPortalServerCards :many
-- Returns one row per MCP server in the project that has an mcp_endpoint
-- (i.e. is addressable). Includes toolset description fallback and tool count
-- from the latest toolset version's tool_urns array.
SELECT
    ms.id              AS server_id,
    ms.name            AS server_name,
    me.slug            AS endpoint_slug,
    me.custom_domain_id AS endpoint_custom_domain_id,
    ts.id              AS toolset_id,
    ts.name            AS toolset_name,
    ts.description     AS toolset_description,
    COALESCE(
        (
            SELECT cardinality(tv.tool_urns)
            FROM toolset_versions tv
            WHERE tv.toolset_id = ts.id
              AND tv.deleted IS FALSE
            ORDER BY tv.version DESC
            LIMIT 1
        ),
        0
    )                  AS tool_count
FROM mcp_servers ms
JOIN mcp_endpoints me ON me.mcp_server_id = ms.id AND me.deleted IS FALSE
LEFT JOIN toolsets ts ON ts.id = ms.toolset_id AND ts.deleted IS FALSE
WHERE ms.project_id = @project_id
  AND ms.deleted IS FALSE
ORDER BY ms.created_at ASC;
