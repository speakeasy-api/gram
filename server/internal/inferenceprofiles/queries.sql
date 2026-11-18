-- name: ListInferenceProfiles :many
SELECT *
FROM inference_profiles
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY lower(name), id;

-- name: GetInferenceProfile :one
SELECT *
FROM inference_profiles
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE;

-- name: GetInferenceProfileForUpdate :one
SELECT *
FROM inference_profiles
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- name: CreateInferenceProfile :one
INSERT INTO inference_profiles (organization_id, name, provider, settings)
VALUES (@organization_id, @name, @provider, @settings)
RETURNING *;

-- name: UpdateInferenceProfile :one
UPDATE inference_profiles
SET name = @name,
    provider = @provider,
    settings = @settings,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteInferenceProfile :one
UPDATE inference_profiles
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteInferenceProfileRulesForProfile :many
UPDATE inference_profile_rules
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND profile_id = @profile_id
  AND deleted IS FALSE
RETURNING *;

-- name: ListInferenceProfileRules :many
SELECT *
FROM inference_profile_rules
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY position, created_at, id;

-- name: ListInferenceProfileRulesForProfile :many
SELECT *
FROM inference_profile_rules
WHERE organization_id = @organization_id
  AND profile_id = @profile_id
  AND deleted IS FALSE
ORDER BY position, created_at, id;

-- name: GetInferenceProfileRuleForUpdate :one
SELECT *
FROM inference_profile_rules
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- name: CreateInferenceProfileRule :one
INSERT INTO inference_profile_rules (organization_id, profile_id, principal_urn, harness, position)
VALUES (@organization_id, @profile_id, @principal_urn, sqlc.narg('harness'), @position)
RETURNING *;

-- name: UpdateInferenceProfileRule :one
UPDATE inference_profile_rules
SET profile_id = @profile_id,
    principal_urn = @principal_urn,
    harness = sqlc.narg('harness'),
    position = @position,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteInferenceProfileRule :one
UPDATE inference_profile_rules
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- name: ListMatchingInferenceProfileRules :many
-- The live rules whose principal is one of the caller's, joined to their live
-- profile, for resolution.
SELECT
  sqlc.embed(r),
  sqlc.embed(p)
FROM inference_profile_rules r
JOIN inference_profiles p
  ON p.organization_id = r.organization_id
  AND p.id = r.profile_id
  AND p.deleted IS FALSE
WHERE r.organization_id = @organization_id
  AND r.principal_urn = ANY(@principal_urns::text[])
  AND r.deleted IS FALSE;
