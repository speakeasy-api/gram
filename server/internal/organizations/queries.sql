-- name: UpsertOrganizationMetadata :one
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    workos_id,
    whitelisted
) VALUES (
    @id,
    @name,
    @slug,
    @workos_id,
    COALESCE(sqlc.narg('whitelisted')::boolean, TRUE)
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    slug = EXCLUDED.slug,
    -- TODO: remove COALESCE once WorkOS org migration is complete and all orgs reliably provide workos_id.
    workos_id = COALESCE(EXCLUDED.workos_id, organization_metadata.workos_id),
    whitelisted = CASE
        WHEN sqlc.narg('whitelisted')::boolean IS NOT NULL THEN sqlc.narg('whitelisted')::boolean
        ELSE organization_metadata.whitelisted
    END,
    updated_at = clock_timestamp()
RETURNING *;

-- name: SetAccountType :exec
UPDATE organization_metadata
SET gram_account_type = @gram_account_type,
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: GetOrganizationMetadata :one
SELECT *
FROM organization_metadata
WHERE id = @id;

-- name: GetOrganizationNameByWorkosID :one
SELECT name
FROM organization_metadata
WHERE workos_id = @workos_id
LIMIT 1;

-- name: UpsertOrganizationUserRelationship :one
INSERT INTO organization_user_relationships (
    organization_id,
    user_id
) VALUES (
    @organization_id,
    @user_id
)
ON CONFLICT (organization_id, user_id) DO UPDATE SET
    updated_at = clock_timestamp()
RETURNING *;

-- name: HasOrganizationUserRelationship :one
SELECT EXISTS(
  SELECT 1
  FROM organization_user_relationships
  WHERE organization_id = @organization_id
    AND user_id = @user_id
    AND deleted_at IS NULL
) AS exists;

-- name: GetOrganizationUserRelationship :one
SELECT *
FROM organization_user_relationships
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND deleted_at IS NULL;

-- name: ListOrganizationUsers :many
SELECT
  our.*,
  u.email AS user_email,
  u.display_name AS user_display_name,
  u.photo_url AS user_photo_url
FROM organization_user_relationships our
JOIN users u ON u.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.deleted_at IS NULL;

-- name: DeleteOrganizationUserRelationship :exec
UPDATE organization_user_relationships
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND user_id = @user_id;

-- name: AttachWorkOSUserToOrg :exec
-- Attach a WorkOS membership ID to an existing organization-user relationship. This is
-- used to link a WorkOS user to an organization in our system. If the relationship
-- doesn't exist, it will be created. If it does exist, the WorkOS membership ID will be
-- updated if it's not already set.
INSERT INTO organization_user_relationships (
    organization_id,
    user_id,
    workos_membership_id
) VALUES (
    @organization_id,
    @user_id,
    @workos_membership_id
)
ON CONFLICT (organization_id, user_id) DO UPDATE SET
    workos_membership_id = COALESCE(organization_user_relationships.workos_membership_id, EXCLUDED.workos_membership_id),
    updated_at = clock_timestamp()
WHERE organization_user_relationships.deleted_at IS NULL;

-- name: SetUserWorkOSMemberships :exec
-- Declaratively set all WorkOS memberships for a user. Takes WorkOS org IDs
-- (not Speakeasy org IDs) and resolves them via organization_metadata. Upserts
-- the provided (workos_org_id, workos_membership_id) pairs and soft-deletes any
-- other relationships where the org has a non-NULL workos_id. Orgs without a
-- workos_id are unaffected. Other users' memberships are never modified.
WITH input_memberships AS (
    SELECT unnest(@workos_org_ids::text[]) AS workos_org_id,
           unnest(@workos_membership_ids::text[]) AS workos_membership_id
),
resolved AS (
    SELECT organization_metadata.id AS organization_id,
           input_memberships.workos_membership_id
    FROM input_memberships
    JOIN organization_metadata ON organization_metadata.workos_id = input_memberships.workos_org_id
),
upserted AS (
    INSERT INTO organization_user_relationships (organization_id, user_id, workos_membership_id)
    SELECT resolved.organization_id, @user_id, resolved.workos_membership_id
    FROM resolved
    ON CONFLICT (organization_id, user_id) DO UPDATE SET
        workos_membership_id = EXCLUDED.workos_membership_id,
        deleted_at = NULL,
        updated_at = clock_timestamp()
    RETURNING organization_id
)
UPDATE organization_user_relationships
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_user_relationships.user_id = @user_id
  AND organization_user_relationships.deleted_at IS NULL
  AND organization_user_relationships.organization_id NOT IN (SELECT organization_id FROM resolved)
  AND organization_user_relationships.organization_id IN (
      SELECT id FROM organization_metadata WHERE workos_id IS NOT NULL
  );

-- name: SetOrgWorkosID :one
UPDATE organization_metadata
SET workos_id = @workos_id,
    updated_at = clock_timestamp()
WHERE id = @organization_id AND
    workos_id IS NULL
RETURNING *;

-- name: ClearOrganizationWorkosID :exec
UPDATE organization_metadata SET workos_id = NULL WHERE id = @organization_id;

-- name: CreateOrganizationMetadata :exec
INSERT INTO organization_metadata (id, name, slug)
VALUES (@id, @name, @slug);

-- name: GetOrganizationByWorkosID :one
SELECT *
FROM organization_metadata
WHERE workos_id = @workos_id
LIMIT 1;

-- name: UpsertOrganizationMetadataFromWorkOS :one
-- Upsert an organization row from a WorkOS organization event. Caller must
-- have already passed the row through ShouldProcessEvent. Sets workos_id and
-- the cursor columns; clears disabled_at so a deleted-then-recreated org
-- comes back online.
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    workos_id,
    workos_updated_at,
    workos_last_event_id
) VALUES (
    @id,
    @name,
    @slug,
    @workos_id,
    @workos_updated_at,
    @workos_last_event_id
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    slug = EXCLUDED.slug,
    workos_id = EXCLUDED.workos_id,
    workos_updated_at = EXCLUDED.workos_updated_at,
    workos_last_event_id = EXCLUDED.workos_last_event_id,
    disabled_at = NULL,
    updated_at = clock_timestamp()
RETURNING *;

-- name: DisableOrganizationByWorkosID :execrows
-- Mark a WorkOS-linked organization as disabled. Append-only: keeps
-- organization_user_relationships intact. Idempotent — disabled_at is only
-- set on first delete event.
UPDATE organization_metadata
SET disabled_at = COALESCE(disabled_at, clock_timestamp()),
    workos_last_event_id = @workos_last_event_id,
    updated_at = clock_timestamp()
WHERE workos_id = @workos_id;
