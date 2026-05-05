-- Queries for authz challenge resolutions.
-- authz_challenge_resolutions is org-scoped (no project_id).

-- name: ListChallengeResolutions :many
-- Returns resolution records for a batch of challenge IDs within an org.
SELECT * FROM authz_challenge_resolutions
WHERE organization_id = @organization_id
  AND challenge_id = ANY(@challenge_ids::text[]);

-- name: InsertChallengeResolution :one
-- Creates a resolution record for a denied challenge.
INSERT INTO authz_challenge_resolutions (
  organization_id, challenge_id, principal_urn, scope,
  resource_kind, resource_id, resolution_type, role_slug, resolved_by
) VALUES (
  @organization_id, @challenge_id, @principal_urn, @scope,
  @resource_kind, @resource_id, @resolution_type, @role_slug, @resolved_by
)
RETURNING *;
