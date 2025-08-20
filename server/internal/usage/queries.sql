-- name: GetPublicServerCount :one
SELECT COUNT(*)
FROM toolsets
WHERE organization_id = @organization_id
  AND mcp_is_public IS TRUE
  AND deleted IS FALSE;