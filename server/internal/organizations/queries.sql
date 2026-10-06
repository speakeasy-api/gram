-- name: UpsertOrganizationMetadataWithRequests :one
WITH written AS (
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    workos_id,
    whitelisted,
    creation_source,
    default_host
) VALUES (
    @id,
    @name,
    @slug,
    @workos_id,
    COALESCE(sqlc.narg('whitelisted')::boolean, FALSE),
    sqlc.narg('creation_source')::text,
    sqlc.narg('default_host')::text
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
    -- The conflict arm is reachable because WorkOS organization sync can insert
    -- the row first, and it records no source. A caller that knows the flow
    -- therefore has to be able to fill that gap. A caller that does not know it
    -- passes null and leaves whatever is already recorded alone, so a later
    -- upsert from an unrelated path cannot erase the flow that created the row.
    creation_source = COALESCE(EXCLUDED.creation_source, organization_metadata.creation_source),
    -- default_host is deliberately absent: it is chosen when the organization
    -- is created, and a later upsert must not move an existing organization's
    -- URLs to another host.
    updated_at = clock_timestamp()
RETURNING *, (xmax = 0) AS inserted
)
SELECT
    (SELECT COALESCE(jsonb_agg(jsonb_build_object('bootstrap_organization_id', id)), '[]'::jsonb) FROM written WHERE inserted)::jsonb AS requests,
    written.id,
    written.name,
    written.slug,
    written.gram_account_type,
    written.workos_id,
    written.workos_updated_at,
    written.workos_last_event_id,
    written.svix_app_id,
    written.webhooks_enabled,
    written.whitelisted,
    written.free_trial_started_at,
    written.free_trial_ends_at,
    written.scim_enabled,
    written.sso_enabled,
    written.verified_domains,
    written.creation_source,
    written.default_host,
    written.created_at,
    written.updated_at,
    written.disabled_at
FROM written;

-- name: SetAccountType :exec
UPDATE organization_metadata
SET gram_account_type = @gram_account_type,
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: SetAccountTypeIfUnchanged :one
UPDATE organization_metadata
SET gram_account_type = @gram_account_type,
    updated_at = clock_timestamp()
WHERE id = @id
  AND gram_account_type = @previous_account_type
  AND gram_account_type NOT IN ('payg', 'enterprise')
RETURNING *;

-- name: GetOrganizationMetadata :one
SELECT *
FROM organization_metadata
WHERE id = @id;

-- name: LockOrganizationForAdminConfiguration :one
-- Pin the displayed identity without blocking settings inserts that acquire
-- foreign-key KEY SHARE locks after the chat-analysis budget lock.
SELECT *
FROM organization_metadata
WHERE id = @id
FOR NO KEY UPDATE;

-- name: LockOrganizationForInviteAcceptance :one
SELECT *
FROM organization_metadata
WHERE id = @id
FOR UPDATE;

-- name: HasOtherActiveOrganizationUsers :one
SELECT EXISTS (
    SELECT 1
    FROM organization_user_relationships AS relationship
    JOIN users ON users.id = relationship.user_id
    WHERE relationship.organization_id = @organization_id
      AND relationship.deleted_at IS NULL
      AND users.deleted_at IS NULL
      AND users.id <> @user_id
);

-- name: GetOrganizationMetadataBySlug :one
SELECT *
FROM organization_metadata
WHERE slug = @slug;

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
    updated_at = clock_timestamp(),
    deleted_at = NULL,
    created_at = CASE WHEN organization_user_relationships.deleted_at IS NOT NULL THEN clock_timestamp() ELSE organization_user_relationships.created_at END
RETURNING *;

-- name: HasOrganizationUserRelationship :one
SELECT EXISTS(
  SELECT 1
  FROM organization_user_relationships
  WHERE organization_id = @organization_id
    AND user_id = @user_id
    AND deleted_at IS NULL
) AS exists;

-- name: HasActiveOrganizationUser :one
-- Returns whether a Gram user is an active member of the organization.
SELECT EXISTS(
  SELECT 1
  FROM users
  JOIN organization_user_relationships
    ON organization_user_relationships.user_id = users.id
  WHERE users.id = @user_id
    AND users.deleted_at IS NULL
    AND organization_user_relationships.organization_id = @organization_id
    AND organization_user_relationships.deleted_at IS NULL
) AS exists;

-- name: LockActiveOrganizationUser :one
SELECT our.user_id
FROM organization_user_relationships AS our
JOIN users AS u ON u.id = our.user_id
WHERE our.user_id = @user_id
  AND our.organization_id = @organization_id
  AND our.deleted_at IS NULL
  AND u.deleted_at IS NULL
FOR SHARE OF our, u;

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
  u.photo_url AS user_photo_url,
  u.last_login AS user_last_login
FROM organization_user_relationships our
JOIN users u ON u.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.deleted_at IS NULL
  AND u.deleted_at IS NULL;

-- name: ListActiveOrganizationUserIDs :many
-- Returns the Gram user IDs of active members of the organization. Used to
-- suppress challenges raised by users outside the organization (e.g. Speakeasy
-- staff impersonating a customer org) from the Challenge UI.
SELECT u.id
FROM organization_user_relationships our
JOIN users u ON u.id = our.user_id
WHERE our.organization_id = @organization_id
  AND our.deleted_at IS NULL
  AND u.deleted_at IS NULL;

-- name: FilterOrganizationMemberUserIDs :many
-- Returns the subset of the given Gram user IDs that are active members of
-- the organization. Used to mask Speakeasy staff identities in customer-facing
-- audit feeds.
SELECT user_id::text AS user_id
FROM organization_user_relationships
WHERE organization_id = @organization_id
  AND user_id = ANY(@user_ids::text[])
  AND deleted_at IS NULL;

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

-- name: SetUserWorkOSMemberships :many
-- Declaratively set all WorkOS memberships for a user. Takes WorkOS org IDs
-- (not Speakeasy org IDs) and resolves them via organization_metadata. Upserts
-- the provided (workos_org_id, workos_membership_id) pairs and, unless
-- preserve_existing is true, soft-deletes any other relationships where the org
-- has a non-NULL workos_id. Other users' memberships are never modified.
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
claimed_placeholders AS (
    UPDATE organization_user_relationships pending
    SET user_id = @user_id,
        deleted_at = NULL,
        updated_at = clock_timestamp()
    FROM resolved
    WHERE pending.workos_membership_id = resolved.workos_membership_id
      AND pending.user_id IS NULL
      AND pending.deleted_at IS NULL
      AND @user_id::text IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM organization_user_relationships existing
          WHERE existing.organization_id = pending.organization_id
            AND existing.user_id = @user_id
            AND existing.deleted_at IS NULL
      )
    RETURNING pending.organization_id, pending.workos_membership_id
),
upserted AS (
    INSERT INTO organization_user_relationships (organization_id, user_id, workos_membership_id)
    SELECT resolved.organization_id, @user_id, resolved.workos_membership_id
    FROM resolved
    WHERE NOT EXISTS (
        SELECT 1
        FROM claimed_placeholders
        WHERE claimed_placeholders.workos_membership_id = resolved.workos_membership_id
    )
    ON CONFLICT (organization_id, user_id) DO UPDATE SET
        workos_membership_id = EXCLUDED.workos_membership_id,
        deleted_at = NULL,
        updated_at = clock_timestamp()
    WHERE organization_user_relationships.deleted IS FALSE
       OR organization_user_relationships.workos_membership_id IS DISTINCT FROM EXCLUDED.workos_membership_id
    RETURNING organization_id
)
UPDATE organization_user_relationships
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_user_relationships.user_id = @user_id
  AND NOT @preserve_existing::boolean
  AND organization_user_relationships.deleted IS FALSE
  AND organization_user_relationships.organization_id NOT IN (SELECT organization_id FROM resolved)
  AND organization_user_relationships.organization_id IN (
      SELECT id FROM organization_metadata WHERE workos_id IS NOT NULL
  )
RETURNING organization_id, user_id;

-- name: SetOrgWorkosID :one
UPDATE organization_metadata
SET workos_id = @workos_id,
    updated_at = clock_timestamp()
WHERE id = @organization_id AND
    workos_id IS NULL
RETURNING *;

-- name: ExpireStaleInvitations :exec
-- Transition pending invitations that have passed their expires_at to 'expired'
-- state. Called before creating a new invitation so the partial unique index
-- (org_id, email) WHERE state = 'pending' does not block re-inviting.
UPDATE organization_invitations
SET state = 'expired',
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND email = @email
  AND state = 'pending'
  AND expires_at <= clock_timestamp();

-- name: CreateInvitation :one
INSERT INTO organization_invitations (
    organization_id,
    email,
    token_hash,
    inviter_user_id,
    role_slug,
    expires_at
) VALUES (
    @organization_id,
    @email,
    @token_hash,
    @inviter_user_id,
    @role_slug,
    clock_timestamp() + make_interval(days => @expires_in_days::int)
)
RETURNING *;

-- name: GetInvitationByID :one
SELECT *
FROM organization_invitations
WHERE id = @id;

-- name: ListPendingInvitations :many
SELECT *
FROM organization_invitations
WHERE organization_id = @organization_id
  AND state = 'pending'
  AND expires_at > clock_timestamp()
ORDER BY created_at DESC;

-- name: RevokeInvitation :exec
UPDATE organization_invitations
SET state = 'revoked',
    revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND state = 'pending';

-- name: RevokeInvitationForOrganization :one
UPDATE organization_invitations
SET state = 'revoked',
    revoked_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND state = 'pending'
RETURNING *;

-- name: UpdateInvitationRole :one
UPDATE organization_invitations
SET role_slug = @role_slug,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND state = 'pending'
  AND expires_at > clock_timestamp()
RETURNING *;

-- name: AcceptInvitation :one
UPDATE organization_invitations
SET state = 'accepted',
    accepted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND state = 'pending'
  AND expires_at > clock_timestamp()
RETURNING *;

-- name: AcceptPendingInvitationForMember :one
UPDATE organization_invitations
SET state = 'accepted',
    accepted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND email = @email
  AND state = 'pending'
  AND expires_at > clock_timestamp()
RETURNING *;

-- name: HasPendingInvitationForEmail :one
-- Reports whether any organization has a live invitation outstanding for this
-- address. It is asked at first-time user creation to tell an invited signup
-- from an organic one, so it deliberately spans every organization rather than
-- one: the user does not exist yet and belongs to none.
SELECT EXISTS (
  SELECT 1
  FROM organization_invitations
  WHERE email = @email
    AND state = 'pending'
    AND expires_at > clock_timestamp()
);

-- name: ExpireInvitationForTest :exec
UPDATE organization_invitations
SET expires_at = clock_timestamp() - interval '1 hour'
WHERE id = @id;

-- name: SetOrganizationDefaultHostForTest :exec
UPDATE organization_metadata
SET default_host = @default_host
WHERE id = @id;

-- name: GetInvitationByTokenHash :one
SELECT *
FROM organization_invitations
WHERE token_hash = @token_hash;

-- name: CreateOrganizationMetadataWithRequests :one
WITH written AS (
INSERT INTO organization_metadata (id, name, slug, default_host)
VALUES (@id, @name, @slug, sqlc.narg('default_host')::text)
RETURNING *, TRUE AS inserted
)
SELECT
    (SELECT COALESCE(jsonb_agg(jsonb_build_object('bootstrap_organization_id', id)), '[]'::jsonb) FROM written WHERE inserted)::jsonb AS requests,
    written.id,
    written.name,
    written.slug,
    written.gram_account_type,
    written.workos_id,
    written.workos_updated_at,
    written.workos_last_event_id,
    written.svix_app_id,
    written.webhooks_enabled,
    written.whitelisted,
    written.free_trial_started_at,
    written.free_trial_ends_at,
    written.scim_enabled,
    written.sso_enabled,
    written.verified_domains,
    written.creation_source,
    written.default_host,
    written.created_at,
    written.updated_at,
    written.disabled_at
FROM written;

-- name: GetOrganizationByWorkosID :one
SELECT *
FROM organization_metadata
WHERE workos_id = @workos_id;

-- name: GetOrganizationRelationshipForUser :one
SELECT *
FROM organization_user_relationships
WHERE organization_id = @organization_id
  AND user_id = @user_id;

-- name: GetRelationshipByMembershipID :one
SELECT *
FROM organization_user_relationships
WHERE workos_membership_id = @workos_membership_id
ORDER BY updated_at DESC
LIMIT 1;

-- name: SetOrganizationRelationshipWorkOSCursor :exec
UPDATE organization_user_relationships
SET workos_updated_at = @workos_updated_at,
    workos_last_event_id = @workos_last_event_id,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND user_id = @user_id;

-- name: UpsertWorkOSMembership :exec
-- Upsert a membership row from a WorkOS organization_membership event. Caller
-- must have already passed the row through ShouldProcessEvent.
WITH updated_existing_user_relationship AS (
    UPDATE organization_user_relationships
    SET workos_user_id = @workos_user_id,
        workos_membership_id = @workos_membership_id,
        workos_updated_at = @workos_updated_at,
        workos_last_event_id = @workos_last_event_id,
        deleted_at = NULL,
        updated_at = clock_timestamp()
    WHERE organization_id = @organization_id
      AND user_id = @user_id
      AND @user_id::text IS NOT NULL
    RETURNING id
)
INSERT INTO organization_user_relationships (
    organization_id,
    user_id,
    workos_user_id,
    workos_membership_id,
    workos_updated_at,
    workos_last_event_id
)
SELECT
    @organization_id,
    @user_id,
    @workos_user_id,
    @workos_membership_id,
    @workos_updated_at,
    @workos_last_event_id
WHERE NOT EXISTS (SELECT 1 FROM updated_existing_user_relationship)
ON CONFLICT (workos_membership_id) WHERE deleted IS FALSE DO UPDATE SET
    user_id = COALESCE(EXCLUDED.user_id, organization_user_relationships.user_id),
    workos_user_id = COALESCE(EXCLUDED.workos_user_id, organization_user_relationships.workos_user_id),
    workos_membership_id = EXCLUDED.workos_membership_id,
    workos_updated_at = EXCLUDED.workos_updated_at,
    workos_last_event_id = EXCLUDED.workos_last_event_id,
    deleted_at = NULL,
    updated_at = clock_timestamp();

-- name: MarkWorkOSMembershipDeleted :many
-- Record a WorkOS membership delete, inserting a tombstone when the local
-- relationship did not exist so stale replayed creates cannot resurrect it.
WITH updated_existing_user_relationship AS (
    UPDATE organization_user_relationships
    SET workos_user_id = @workos_user_id,
        workos_membership_id = @workos_membership_id,
        workos_updated_at = @workos_updated_at,
        workos_last_event_id = @workos_last_event_id,
        deleted_at = COALESCE(deleted_at, clock_timestamp()),
        updated_at = clock_timestamp()
    WHERE organization_user_relationships.organization_id = @organization_id
      AND (
          (sqlc.narg('user_id')::text IS NOT NULL AND organization_user_relationships.user_id = sqlc.narg('user_id'))
          OR (@workos_user_id::text IS NOT NULL AND organization_user_relationships.workos_user_id = @workos_user_id)
          OR (@workos_membership_id::text IS NOT NULL AND organization_user_relationships.workos_membership_id = @workos_membership_id)
      )
    RETURNING organization_user_relationships.user_id
),
inserted AS (
INSERT INTO organization_user_relationships (
    organization_id,
    user_id,
    workos_user_id,
    workos_membership_id,
    workos_updated_at,
    workos_last_event_id,
    deleted_at
)
SELECT
    @organization_id,
    sqlc.narg('user_id'),
    @workos_user_id,
    @workos_membership_id,
    @workos_updated_at,
    @workos_last_event_id,
    clock_timestamp()
WHERE NOT EXISTS (SELECT 1 FROM updated_existing_user_relationship)
ON CONFLICT (workos_membership_id) WHERE deleted IS FALSE DO UPDATE SET
    user_id = COALESCE(EXCLUDED.user_id, organization_user_relationships.user_id),
    workos_user_id = COALESCE(EXCLUDED.workos_user_id, organization_user_relationships.workos_user_id),
    workos_membership_id = COALESCE(EXCLUDED.workos_membership_id, organization_user_relationships.workos_membership_id),
    workos_updated_at = EXCLUDED.workos_updated_at,
    workos_last_event_id = EXCLUDED.workos_last_event_id,
    deleted_at = COALESCE(organization_user_relationships.deleted_at, clock_timestamp()),
    updated_at = clock_timestamp()
RETURNING user_id
)
SELECT user_id FROM updated_existing_user_relationship
UNION ALL
SELECT user_id FROM inserted;

-- name: SyncUserOrganizationRoleAssignments :exec
-- Declaratively set all WorkOS role assignments for a known Gram user in an
-- org. Role slugs are resolved from role sync tables and stale assignments for
-- this WorkOS user are removed.
WITH input_role_urns AS (
    SELECT 'role:organization:' || id::text AS role_urn
    FROM organization_roles
    WHERE organization_id = @organization_id
      AND workos_slug = ANY(@workos_role_slugs::text[])
      AND deleted IS FALSE
      AND workos_deleted IS FALSE
    UNION ALL
    SELECT 'role:global:' || id::text AS role_urn
    FROM global_roles
    WHERE workos_slug = ANY(@workos_role_slugs::text[])
      AND deleted IS FALSE
      AND workos_deleted IS FALSE
),
upserted AS (
    INSERT INTO organization_role_assignments (
        organization_id,
        workos_user_id,
        user_id,
        role_urn,
        workos_membership_id,
        workos_updated_at,
        workos_last_event_id
    )
    SELECT
        @organization_id,
        @workos_user_id,
        @user_id,
        input_role_urns.role_urn,
        @workos_membership_id,
        @workos_updated_at,
        @workos_last_event_id
    FROM input_role_urns
    ON CONFLICT (organization_id, workos_user_id, role_urn) WHERE deleted_at IS NULL DO UPDATE SET
        -- COALESCE preserves a backfilled user_id if the sync fires before the Gram user exists.
        user_id = COALESCE(EXCLUDED.user_id, organization_role_assignments.user_id),
        workos_membership_id = EXCLUDED.workos_membership_id,
        workos_updated_at = EXCLUDED.workos_updated_at,
        workos_last_event_id = EXCLUDED.workos_last_event_id,
        deleted_at = NULL,
        updated_at = clock_timestamp()
    RETURNING role_urn
)
UPDATE organization_role_assignments
SET workos_updated_at = @workos_updated_at,
    workos_last_event_id = @workos_last_event_id,
    deleted_at = COALESCE(deleted_at, clock_timestamp()),
    updated_at = clock_timestamp()
WHERE organization_role_assignments.organization_id = @organization_id
  AND organization_role_assignments.workos_user_id = @workos_user_id
  AND organization_role_assignments.deleted_at IS NULL
  AND organization_role_assignments.role_urn NOT IN (SELECT input_role_urns.role_urn FROM input_role_urns);

-- name: MarkRoleAssignmentsDeleted :exec
UPDATE organization_role_assignments
SET workos_updated_at = @workos_updated_at,
    workos_last_event_id = @workos_last_event_id,
    deleted_at = COALESCE(deleted_at, clock_timestamp()),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND workos_user_id = @workos_user_id
  AND deleted_at IS NULL;

-- name: LinkRoleAssignmentsToUser :exec
UPDATE organization_role_assignments
SET user_id = @user_id,
    updated_at = clock_timestamp()
WHERE workos_user_id = @workos_user_id
  AND user_id IS NULL
  AND deleted_at IS NULL;

-- name: LinkRelationshipsToUser :exec
WITH pending_relationships AS (
    SELECT
        id,
        organization_id,
        workos_user_id,
        workos_membership_id,
        workos_updated_at,
        workos_last_event_id
    FROM organization_user_relationships
    WHERE workos_user_id = @workos_user_id
      AND user_id IS NULL
      AND deleted_at IS NULL
),
deleted_pending_for_tombstones AS (
    UPDATE organization_user_relationships pending
    SET deleted_at = clock_timestamp(),
        updated_at = clock_timestamp()
    FROM pending_relationships
    WHERE pending.id = pending_relationships.id
      AND EXISTS (
          SELECT 1
          FROM organization_user_relationships existing
          WHERE existing.organization_id = pending_relationships.organization_id
            AND existing.user_id = @user_id
            AND existing.deleted_at IS NOT NULL
      )
    RETURNING
        pending_relationships.organization_id,
        pending_relationships.workos_user_id,
        pending_relationships.workos_membership_id,
        pending_relationships.workos_updated_at,
        pending_relationships.workos_last_event_id
),
relinked_tombstones AS (
    UPDATE organization_user_relationships existing
    SET workos_user_id = deleted_pending_for_tombstones.workos_user_id,
        workos_membership_id = deleted_pending_for_tombstones.workos_membership_id,
        workos_updated_at = deleted_pending_for_tombstones.workos_updated_at,
        workos_last_event_id = deleted_pending_for_tombstones.workos_last_event_id,
        deleted_at = NULL,
        updated_at = clock_timestamp()
    FROM deleted_pending_for_tombstones
    WHERE existing.organization_id = deleted_pending_for_tombstones.organization_id
      AND existing.user_id = @user_id
      AND existing.deleted_at IS NOT NULL
)
UPDATE organization_user_relationships pending
SET user_id = @user_id,
    updated_at = clock_timestamp()
WHERE pending.workos_user_id = @workos_user_id
  AND pending.user_id IS NULL
  AND pending.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM organization_user_relationships existing
      WHERE existing.organization_id = pending.organization_id
        AND existing.user_id = @user_id
  );

-- name: GetRoleAssignmentLinkedToDifferentWorkOSUser :one
SELECT id, workos_user_id
FROM organization_role_assignments
WHERE user_id = @user_id
  AND workos_user_id <> @workos_user_id
ORDER BY updated_at DESC
LIMIT 1;

-- name: SetOrganizationUserWorkOSID :exec
UPDATE organization_user_relationships
SET workos_user_id = @workos_user_id,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND user_id = @user_id
  AND deleted_at IS NULL;

-- name: ReassignOrganizationUserWorkOSID :exec
-- Login reuses a Gram user after WorkOS delete-and-signup, so membership
-- rows still pointing at a previous WorkOS user id must follow the new one.
-- Matches any leftover id so a retry after overwrite still converges.
UPDATE organization_user_relationships
SET workos_user_id = @new_workos_user_id,
    updated_at = clock_timestamp()
WHERE user_id = @user_id
  AND workos_user_id IS NOT NULL
  AND workos_user_id IS DISTINCT FROM @new_workos_user_id;

-- name: RetireCollidingOrganizationRoleAssignments :exec
-- Soft-delete leftover assignments that would unique-violate if remapped
-- onto a WorkOS id that already holds the same org+role.
UPDATE organization_role_assignments AS old
SET deleted_at = COALESCE(old.deleted_at, clock_timestamp()),
    updated_at = clock_timestamp()
WHERE old.user_id = @user_id
  AND old.workos_user_id <> @new_workos_user_id
  AND old.deleted_at IS NULL
  AND EXISTS (
      SELECT 1
      FROM organization_role_assignments AS neu
      WHERE neu.organization_id = old.organization_id
        AND neu.workos_user_id = @new_workos_user_id
        AND neu.role_urn = old.role_urn
        AND neu.deleted_at IS NULL
  );

-- name: RetireDuplicateLeftoverOrganizationRoleAssignments :exec
-- Soft-delete extra leftover assignments that share org+role so remapping
-- them onto one WorkOS id cannot unique-violate. Keeps the newest leftover.
UPDATE organization_role_assignments AS dest
SET deleted_at = COALESCE(dest.deleted_at, clock_timestamp()),
    updated_at = clock_timestamp()
FROM (
  SELECT id
  FROM (
    SELECT leftover.id,
           ROW_NUMBER() OVER (
             PARTITION BY leftover.organization_id, leftover.role_urn
             ORDER BY leftover.created_at DESC, leftover.id DESC
           ) AS rn
    FROM organization_role_assignments leftover
    WHERE leftover.user_id = sqlc.arg(user_id)
      AND leftover.workos_user_id <> sqlc.arg(new_workos_user_id)
      AND leftover.deleted_at IS NULL
  ) ranked
  WHERE rn > 1
) extras
WHERE dest.id = extras.id;

-- name: ReassignOrganizationRoleAssignmentWorkOSID :exec
-- Move leftover assignments onto the new WorkOS id after colliding rows
-- have been retired.
UPDATE organization_role_assignments
SET workos_user_id = @new_workos_user_id,
    updated_at = clock_timestamp()
WHERE user_id = @user_id
  AND workos_user_id <> @new_workos_user_id
  AND deleted_at IS NULL;

-- name: ListOrganizationRoleAssignmentsByWorkOSUser :many
SELECT *
FROM organization_role_assignments
WHERE organization_id = @organization_id
  AND workos_user_id = @workos_user_id
ORDER BY role_urn;

-- name: LockOrganizationSlug :exec
SELECT pg_advisory_xact_lock(hashtext(@slug));

-- name: CreateOrganizationMetadataFromWorkOSWithRequests :one
-- Create a Gram organization row from a WorkOS organization event. The caller
-- chooses the Gram org ID from WorkOS external_id or a deterministic fallback.
-- Slug is a Gram-owned initial value and is never updated by WorkOS sync.
WITH written AS (
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    workos_id,
    workos_updated_at,
    workos_last_event_id,
    verified_domains,
    default_host
) VALUES (
    @id,
    @name,
    @slug,
    @workos_id,
    @workos_updated_at,
    @workos_last_event_id,
    @verified_domains::text[],
    sqlc.narg('default_host')::text
)
RETURNING *, (xmax = 0) AS inserted
)
SELECT
    (SELECT COALESCE(jsonb_agg(jsonb_build_object('bootstrap_organization_id', id)), '[]'::jsonb) FROM written WHERE inserted)::jsonb AS requests,
    written.id,
    written.name,
    written.slug,
    written.gram_account_type,
    written.workos_id,
    written.workos_updated_at,
    written.workos_last_event_id,
    written.svix_app_id,
    written.webhooks_enabled,
    written.whitelisted,
    written.free_trial_started_at,
    written.free_trial_ends_at,
    written.scim_enabled,
    written.sso_enabled,
    written.verified_domains,
    written.creation_source,
    written.default_host,
    written.created_at,
    written.updated_at,
    written.disabled_at
FROM written;

-- name: UpsertOrganizationMetadataFromWorkOSWithRequests :one
-- Upsert a Gram organization row from a WorkOS organization event.
-- The caller must only use this when WorkOS external_id is set and is the Gram
-- org ID. Slug is a Gram-owned initial value chosen by the caller and is never
-- updated by WorkOS sync after creation.
WITH written AS (
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    workos_id,
    workos_updated_at,
    workos_last_event_id,
    default_host
) VALUES (
    @id,
    @name,
    @slug,
    @workos_id,
    @workos_updated_at,
    @workos_last_event_id,
    sqlc.narg('default_host')::text
)
ON CONFLICT (id) DO UPDATE SET
    -- default_host is only written on insert, so an existing organization
    -- keeps its host.
    name = EXCLUDED.name,
    workos_id = EXCLUDED.workos_id,
    workos_updated_at = EXCLUDED.workos_updated_at,
    workos_last_event_id = EXCLUDED.workos_last_event_id,
    updated_at = clock_timestamp()
RETURNING *, (xmax = 0) AS inserted
)
SELECT
    (SELECT COALESCE(jsonb_agg(jsonb_build_object('bootstrap_organization_id', id)), '[]'::jsonb) FROM written WHERE inserted)::jsonb AS requests,
    written.id,
    written.name,
    written.slug,
    written.gram_account_type,
    written.workos_id,
    written.workos_updated_at,
    written.workos_last_event_id,
    written.svix_app_id,
    written.webhooks_enabled,
    written.whitelisted,
    written.free_trial_started_at,
    written.free_trial_ends_at,
    written.scim_enabled,
    written.sso_enabled,
    written.verified_domains,
    written.creation_source,
    written.default_host,
    written.created_at,
    written.updated_at,
    written.disabled_at
FROM written;

-- name: UpdateOrganizationMetadataFromWorkOS :one
-- Update an existing organization row from a WorkOS organization event. Caller
-- must have already resolved the Gram organization and passed the row through
-- ShouldProcessEvent. WorkOS does not own Gram slugs, so this only updates
-- WorkOS-owned metadata and cursor columns.
UPDATE organization_metadata
SET name = @name,
    workos_id = @workos_id,
    workos_updated_at = @workos_updated_at,
    workos_last_event_id = @workos_last_event_id,
    verified_domains = @verified_domains::text[],
    updated_at = clock_timestamp()
WHERE id = @id
RETURNING *;

-- name: ListOrganizationsForUser :many
SELECT om.id, om.name, om.slug, om.workos_id, om.sso_enabled, om.scim_enabled
FROM organization_user_relationships our
JOIN organization_metadata om ON om.id = our.organization_id
WHERE our.user_id = @user_id
  AND our.deleted_at IS NULL
  AND om.disabled_at IS NULL;

-- name: DisableOrganizationByWorkosID :execrows
-- Mark a WorkOS-linked organization as disabled. Append-only: keeps
-- organization_user_relationships intact. Idempotent — disabled_at is only
-- set on first delete event.
UPDATE organization_metadata
SET disabled_at = COALESCE(disabled_at, clock_timestamp()),
    workos_last_event_id = @workos_last_event_id,
    updated_at = clock_timestamp()
WHERE workos_id = @workos_id;


-- name: UpsertSvixAppID :one
WITH previous AS (
    SELECT prev.svix_app_id
    FROM organization_metadata prev
    WHERE
        prev.id = @id
        AND prev.disabled_at IS NULL
)
UPDATE organization_metadata om
SET svix_app_id = @svix_app_id,
    webhooks_enabled = TRUE,
    updated_at = clock_timestamp()
WHERE
    om.id = @id
    AND om.disabled_at IS NULL
RETURNING
    om.id,
    om.svix_app_id,
    (SELECT previous.svix_app_id FROM previous) AS previous_svix_app_id,
    om.webhooks_enabled;

-- name: SetWebhooksEnabled :one
UPDATE organization_metadata
SET webhooks_enabled = @enabled,
    updated_at = clock_timestamp()
WHERE
    id = @id
    AND COALESCE(webhooks_enabled, FALSE) IS DISTINCT FROM @enabled
RETURNING id, svix_app_id, webhooks_enabled;

-- name: GetSvixAppID :one
SELECT svix_app_id
FROM organization_metadata
WHERE id = @id AND svix_app_id IS NOT NULL;

-- name: SetSSOEnabled :exec
-- Update the SSO enabled flag on an organization. Called when a WorkOS
-- connection.activated or connection.deactivated/deleted event is processed.
UPDATE organization_metadata
SET sso_enabled = @enabled,
    workos_last_event_id = @workos_last_event_id,
    updated_at = clock_timestamp()
WHERE workos_id = @workos_id;

-- name: SetSCIMEnabled :exec
-- Update the SCIM/directory sync enabled flag on an organization. Called when
-- a WorkOS dsync.activated or dsync.deleted event is processed.
UPDATE organization_metadata
SET scim_enabled = @enabled,
    workos_last_event_id = @workos_last_event_id,
    updated_at = clock_timestamp()
WHERE workos_id = @workos_id;

-- name: AddVerifiedDomainByWorkosID :exec
-- Add one domain to an organization's verified domains after a WorkOS
-- organization_domain.verified event. The match ignores case, so a domain
-- already in the list is not added twice. The event cursor is recorded even
-- when the list does not change.
UPDATE organization_metadata
SET verified_domains = CASE
        WHEN EXISTS (
            SELECT 1
            FROM unnest(COALESCE(organization_metadata.verified_domains, '{}'::text[])) AS existing (domain)
            WHERE lower(existing.domain) = lower(@domain::text)
        ) THEN COALESCE(organization_metadata.verified_domains, '{}'::text[])
        ELSE array_append(COALESCE(organization_metadata.verified_domains, '{}'::text[]), @domain::text)
    END,
    workos_last_event_id = @workos_last_event_id,
    updated_at = clock_timestamp()
WHERE workos_id = @workos_id;

-- name: RemoveVerifiedDomainByWorkosID :exec
-- Remove one domain from an organization's verified domains after a WorkOS
-- organization_domain.deleted event. The match ignores case and keeps the
-- order of the remaining domains. The event cursor is recorded even when the
-- domain was not in the list.
UPDATE organization_metadata
SET verified_domains = ARRAY(
        SELECT existing.domain
        FROM unnest(COALESCE(organization_metadata.verified_domains, '{}'::text[])) WITH ORDINALITY AS existing (domain, position)
        WHERE lower(existing.domain) <> lower(@domain::text)
        ORDER BY existing.position
    ),
    workos_last_event_id = @workos_last_event_id,
    updated_at = clock_timestamp()
WHERE workos_id = @workos_id;

-- name: SetVerifiedDomains :exec
-- Fill an empty verified domains list with the result of a live WorkOS check.
-- A non-empty list is owned by the event sync and may be newer than the live
-- check, so it is never overwritten here.
UPDATE organization_metadata
SET verified_domains = @verified_domains::text[],
    updated_at = clock_timestamp()
WHERE id = @id
  AND cardinality(COALESCE(verified_domains, '{}'::text[])) = 0;

-- name: ClearWorkosOrgID :exec
UPDATE organization_metadata
SET workos_id = NULL,
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: ListActiveRoleAssignmentsByOrganization :many
SELECT
    ora.user_id,
    ora.role_urn,
    COALESCE(orgr.workos_name, gr.workos_name, ora.role_urn) AS role_name
FROM organization_role_assignments ora
LEFT JOIN organization_roles orgr
    ON ora.role_urn = 'role:organization:' || orgr.id::text
    AND orgr.organization_id = @organization_id
    AND orgr.deleted IS FALSE
LEFT JOIN global_roles gr
    ON ora.role_urn = 'role:global:' || gr.id::text
    AND gr.deleted IS FALSE
WHERE ora.organization_id = @organization_id
  AND ora.user_id IS NOT NULL
  AND ora.deleted_at IS NULL;

-- name: ListOrganizationSetupTasks :many
SELECT *
FROM organization_setup_tasks
WHERE organization_id = @organization_id;

-- name: LockOrganizationForSetupTaskUpdate :one
SELECT *
FROM organization_metadata
WHERE id = @organization_id
FOR UPDATE;


-- name: GetOrganizationSetupTask :one
SELECT *
FROM organization_setup_tasks
WHERE organization_id = @organization_id
  AND task_key = @task_key;

-- name: UpsertOrganizationSetupTask :one
INSERT INTO organization_setup_tasks (
    organization_id,
    task_key,
    status,
    assignee_user_id,
    assignee_email,
    hidden_at
) VALUES (
    @organization_id,
    @task_key,
    @status,
    sqlc.narg('assignee_user_id')::text,
    sqlc.narg('assignee_email')::text,
    sqlc.narg('hidden_at')::timestamptz
)
ON CONFLICT (organization_id, task_key) DO UPDATE SET
    status = EXCLUDED.status,
    assignee_user_id = EXCLUDED.assignee_user_id,
    assignee_email = EXCLUDED.assignee_email,
    hidden_at = EXCLUDED.hidden_at,
    updated_at = clock_timestamp()
RETURNING *;

-- name: GetSetupTaskCompletionFacts :one
WITH default_project AS (
    SELECT id
    FROM projects
    WHERE organization_id = @organization_id
      AND deleted IS FALSE
    ORDER BY created_at, id
    LIMIT 1
)
SELECT
    COALESCE(organization_metadata.sso_enabled, FALSE)::boolean AS sso_configured,
    COALESCE(organization_metadata.scim_enabled, FALSE)::boolean AS dsync_configured,
    (COALESCE(cardinality(organization_metadata.verified_domains), 0) > 0)::boolean AS domain_verified,
    EXISTS (
        SELECT 1
        FROM plugin_github_connections
        JOIN default_project ON default_project.id = plugin_github_connections.project_id
        WHERE NULLIF(plugin_github_connections.marketplace_token, '') IS NOT NULL
    ) AS marketplace_published,
    (
        SELECT COUNT(DISTINCT organization_features.feature_name) = 3
        FROM organization_features
        WHERE organization_features.organization_id = @organization_id
          AND organization_features.feature_name IN ('logs', 'tool_io_logs', 'session_capture')
          AND organization_features.deleted IS FALSE
    )::boolean AS logging_enabled
FROM organization_metadata
WHERE organization_metadata.id = @organization_id;

-- name: GetOrganizationOnboardingSelection :many
SELECT onboarding.preset AS onboarding_preset, task.task_key, task.hidden_at
FROM organization_metadata om
LEFT JOIN organization_onboarding onboarding ON onboarding.organization_id = om.id
LEFT JOIN organization_setup_tasks task ON task.organization_id = om.id
WHERE om.id = @organization_id;

-- name: SetOrganizationOnboardingPreset :exec
INSERT INTO organization_onboarding (organization_id, preset)
VALUES (@organization_id::text, @preset)
ON CONFLICT (organization_id) DO UPDATE SET
    preset = EXCLUDED.preset,
    updated_at = clock_timestamp();

-- name: SetOrganizationSetupTaskVisibility :exec
INSERT INTO organization_setup_tasks (organization_id, task_key, status, hidden_at)
VALUES (@organization_id, @task_key, 'todo', CASE WHEN @hidden::boolean THEN clock_timestamp() ELSE NULL END)
ON CONFLICT (organization_id, task_key) DO UPDATE SET
    hidden_at = EXCLUDED.hidden_at,
    updated_at = clock_timestamp()
WHERE (organization_setup_tasks.hidden_at IS NOT NULL) IS DISTINCT FROM @hidden::boolean;

-- name: LockOnboardingSteps :exec
SELECT pg_advisory_xact_lock(719438202);

-- name: UpsertOnboardingStep :one
INSERT INTO onboarding_steps (slug, title, description, completion, hidden_by_default, sort_order)
VALUES (@slug, @title, @description, @completion, @hidden_by_default, @sort_order)
ON CONFLICT (slug) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    completion = EXCLUDED.completion,
    hidden_by_default = EXCLUDED.hidden_by_default,
    sort_order = EXCLUDED.sort_order,
    deleted_at = NULL,
    updated_at = clock_timestamp()
RETURNING id;

-- name: SetOnboardingStepParent :exec
UPDATE onboarding_steps
SET parent_step_id = sqlc.narg(parent_step_id), updated_at = clock_timestamp()
WHERE id = @id AND parent_step_id IS DISTINCT FROM sqlc.narg(parent_step_id);

-- name: RetireOnboardingStepsNotIn :exec
UPDATE onboarding_steps
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE deleted_at IS NULL AND NOT (slug = ANY(@slugs::text[]));

-- name: DeleteOnboardingStepMethods :exec
DELETE FROM onboarding_step_methods WHERE step_id = @step_id;

-- name: InsertOnboardingStepMethod :execrows
INSERT INTO onboarding_step_methods (step_id, integration_method_id)
SELECT @step_id, m.id
FROM support_matrix_integration_methods m
WHERE m.slug = @method_slug AND m.deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- name: DeleteOnboardingStepDependencies :exec
DELETE FROM onboarding_step_dependencies WHERE step_id = @step_id;

-- name: InsertOnboardingStepDependency :exec
INSERT INTO onboarding_step_dependencies (step_id, requires_step_id)
SELECT @step_id, r.id
FROM onboarding_steps r
WHERE r.slug = @requires_slug
ON CONFLICT DO NOTHING;

-- name: ListOnboardingSteps :many
SELECT s.id, s.slug, s.title, s.description, s.completion, s.hidden_by_default, s.sort_order,
  p.slug AS parent_slug,
  (
    SELECT coalesce(array_agg(m.slug ORDER BY m.sort_order, m.slug), '{}')::text[]
    FROM onboarding_step_methods sm
    JOIN support_matrix_integration_methods m ON m.id = sm.integration_method_id AND m.deleted_at IS NULL
    WHERE sm.step_id = s.id
  ) AS method_slugs,
  (
    SELECT coalesce(array_agg(r.slug ORDER BY r.sort_order, r.slug), '{}')::text[]
    FROM onboarding_step_dependencies d
    JOIN onboarding_steps r ON r.id = d.requires_step_id AND r.deleted_at IS NULL
    WHERE d.step_id = s.id
  ) AS requires_slugs
FROM onboarding_steps s
LEFT JOIN onboarding_steps p ON p.id = s.parent_step_id AND p.deleted_at IS NULL
WHERE s.deleted_at IS NULL
ORDER BY s.sort_order, s.slug;

-- name: GetOrganizationOnboardingStack :one
SELECT om.id, om.name, om.slug, onboarding.mdm_vendor, onboarding.mdm_vendor_name
FROM organization_metadata om
LEFT JOIN organization_onboarding onboarding ON onboarding.organization_id = om.id
WHERE om.id = @organization_id;

-- name: ListOrganizationOnboardingVendors :many
SELECT v.vendor, p.slug AS plan_slug
FROM organization_onboarding_vendors v
LEFT JOIN support_matrix_plans p ON p.id = v.plan_id AND p.deleted_at IS NULL
WHERE v.organization_id = @organization_id
ORDER BY v.vendor;

-- name: UpsertOrganizationOnboardingStack :exec
INSERT INTO organization_onboarding (organization_id, mdm_vendor, mdm_vendor_name)
VALUES (@organization_id::text, sqlc.narg(mdm_vendor), sqlc.narg(mdm_vendor_name))
ON CONFLICT (organization_id) DO UPDATE SET
    mdm_vendor = EXCLUDED.mdm_vendor,
    mdm_vendor_name = EXCLUDED.mdm_vendor_name,
    updated_at = clock_timestamp();

-- name: DeleteOrganizationOnboardingVendors :exec
DELETE FROM organization_onboarding_vendors WHERE organization_id = @organization_id;

-- name: InsertOrganizationOnboardingVendor :exec
INSERT INTO organization_onboarding_vendors (organization_id, vendor, plan_id)
VALUES (
  @organization_id,
  @vendor,
  (SELECT p.id FROM support_matrix_plans p WHERE p.slug = sqlc.narg(plan_slug)::text AND p.deleted_at IS NULL)
);

-- name: ListSupportMatrixPlatformsForOnboarding :many
SELECT slug, name, vendor, family, surface
FROM support_matrix_platforms
WHERE deleted_at IS NULL
ORDER BY sort_order, slug;

-- name: ListSupportMatrixPlansForOnboarding :many
SELECT slug, vendor, name
FROM support_matrix_plans
WHERE deleted_at IS NULL
ORDER BY sort_order, slug;

-- name: LockOnboardingPlaybooks :exec
SELECT pg_advisory_xact_lock(719438203);

-- name: ListOnboardingUseCases :many
SELECT u.id, u.slug, u.name, u.description, u.sort_order,
  d.id AS default_playbook_id
FROM onboarding_use_cases u
LEFT JOIN onboarding_playbooks d ON d.use_case_id = u.id AND d.is_default AND d.organization_id IS NULL AND d.deleted_at IS NULL
WHERE u.deleted_at IS NULL
ORDER BY u.sort_order, u.name, u.id;

-- name: GetOnboardingUseCase :one
SELECT id, slug, name, description, sort_order
FROM onboarding_use_cases
WHERE id = @id AND deleted_at IS NULL;

-- name: GetOnboardingUseCaseBySlug :one
SELECT id, slug, name, description, sort_order
FROM onboarding_use_cases
WHERE slug = @slug AND deleted_at IS NULL;

-- name: CreateOnboardingUseCase :one
INSERT INTO onboarding_use_cases (slug, name, description, sort_order)
VALUES (@slug, @name, @description, (SELECT coalesce(max(sort_order), 0) + 1 FROM onboarding_use_cases))
RETURNING id, slug, name, description, sort_order;

-- name: UpdateOnboardingUseCase :one
UPDATE onboarding_use_cases
SET name = @name, description = @description, updated_at = clock_timestamp()
WHERE id = @id AND deleted_at IS NULL
RETURNING id, slug, name, description, sort_order;

-- name: DeleteOnboardingUseCase :execrows
UPDATE onboarding_use_cases
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND deleted_at IS NULL;

-- name: DeleteOnboardingPlaybooksOfUseCase :exec
UPDATE onboarding_playbooks
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE use_case_id = @use_case_id AND deleted_at IS NULL;

-- name: ListOnboardingPlaybooks :many
-- Every playbook, or, for an organization, the use cases' and its own. A
-- playbook belongs to a use case or to an organization, never both.
SELECT p.id, p.use_case_id, p.organization_id, p.name, p.description, p.is_default,
  u.slug AS use_case_slug, u.name AS use_case_name,
  om.name AS organization_name
FROM onboarding_playbooks p
LEFT JOIN onboarding_use_cases u ON u.id = p.use_case_id AND u.deleted_at IS NULL
LEFT JOIN organization_metadata om ON om.id = p.organization_id
WHERE p.deleted_at IS NULL
  AND (p.use_case_id IS NULL OR u.id IS NOT NULL)
  AND (
    sqlc.narg(organization_id)::text IS NULL
    OR p.organization_id IS NULL
    OR p.organization_id = sqlc.narg(organization_id)::text
  )
ORDER BY p.organization_id NULLS FIRST, u.sort_order, u.name, om.name, p.is_default DESC, p.name, p.id;

-- name: GetOnboardingPlaybook :one
SELECT p.id, p.use_case_id, p.organization_id, p.name, p.description, p.is_default,
  u.slug AS use_case_slug, u.name AS use_case_name,
  om.name AS organization_name
FROM onboarding_playbooks p
LEFT JOIN onboarding_use_cases u ON u.id = p.use_case_id AND u.deleted_at IS NULL
LEFT JOIN organization_metadata om ON om.id = p.organization_id
WHERE p.id = @id AND p.deleted_at IS NULL
  AND (p.use_case_id IS NULL OR u.id IS NOT NULL);

-- name: GetOnboardingDefaultPlaybook :one
SELECT p.id, p.use_case_id, p.organization_id, p.name, p.description, p.is_default,
  u.slug AS use_case_slug, u.name AS use_case_name
FROM onboarding_playbooks p
JOIN onboarding_use_cases u ON u.id = p.use_case_id AND u.deleted_at IS NULL
WHERE p.use_case_id = @use_case_id AND p.is_default AND p.organization_id IS NULL AND p.deleted_at IS NULL;

-- name: ListOnboardingPlaybookSteps :many
SELECT ps.playbook_id, s.slug, s.title, ps.position
FROM onboarding_playbook_steps ps
JOIN onboarding_steps s ON s.id = ps.step_id AND s.deleted_at IS NULL
WHERE ps.playbook_id = ANY(@playbook_ids::uuid[])
ORDER BY ps.playbook_id, ps.position, s.slug;

-- name: CreateOnboardingPlaybook :one
INSERT INTO onboarding_playbooks (use_case_id, organization_id, name, description, is_default)
VALUES (sqlc.narg(use_case_id)::uuid, sqlc.narg(organization_id)::text, @name, @description, @is_default)
RETURNING id, use_case_id, organization_id, name, description, is_default;

-- name: UpdateOnboardingPlaybook :one
UPDATE onboarding_playbooks
SET name = @name, description = @description, is_default = @is_default, updated_at = clock_timestamp()
WHERE id = @id AND deleted_at IS NULL
RETURNING id, use_case_id, organization_id, name, description, is_default;

-- name: ClearOnboardingDefaultPlaybook :exec
UPDATE onboarding_playbooks
SET is_default = false, updated_at = clock_timestamp()
WHERE use_case_id = @use_case_id AND organization_id IS NULL AND is_default AND deleted_at IS NULL AND id <> @keep_id;

-- name: DeleteOnboardingPlaybook :execrows
UPDATE onboarding_playbooks
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE id = @id AND deleted_at IS NULL;

-- name: DeleteOnboardingPlaybookSteps :exec
DELETE FROM onboarding_playbook_steps WHERE playbook_id = @playbook_id;

-- name: InsertOnboardingPlaybookStep :execrows
INSERT INTO onboarding_playbook_steps (playbook_id, step_id, position)
SELECT @playbook_id, s.id, @position
FROM onboarding_steps s
WHERE s.slug = @slug AND s.deleted_at IS NULL;

-- name: GetOrganizationOnboardingPlaybookID :one
SELECT om.id AS organization_id, o.playbook_id
FROM organization_metadata om
LEFT JOIN organization_onboarding o ON o.organization_id = om.id
WHERE om.id = @organization_id;

-- name: SetOrganizationOnboardingPlaybook :exec
INSERT INTO organization_onboarding (organization_id, playbook_id)
VALUES (@organization_id::text, sqlc.narg(playbook_id)::uuid)
ON CONFLICT (organization_id) DO UPDATE SET
    playbook_id = EXCLUDED.playbook_id,
    updated_at = clock_timestamp();

-- name: ListOrganizationOnboardingPlaybookSteps :many
SELECT s.slug
FROM organization_onboarding o
JOIN onboarding_playbooks p ON p.id = o.playbook_id AND p.deleted_at IS NULL
JOIN onboarding_playbook_steps ps ON ps.playbook_id = p.id
JOIN onboarding_steps s ON s.id = ps.step_id AND s.deleted_at IS NULL
WHERE o.organization_id = @organization_id
ORDER BY ps.position, s.slug;

-- name: ListOnboardingStepMethodApplicability :many
SELECT s.slug AS step_slug, m.slug AS method_slug, m.vendor AS method_vendor,
  -- Over the stack's platforms of the method's own vendor, or of every vendor
  -- for a method that belongs to none, so an unrelated vendor in the stack
  -- never changes the verdict. A platform the matrix does not map the method
  -- to is unknown, which is not the same as not applicable.
  coalesce((
    SELECT bool_and(mp.platform_id IS NOT NULL AND mp.applicability = 'na')
    FROM support_matrix_platforms p
    LEFT JOIN support_matrix_method_platforms mp ON mp.platform_id = p.id AND mp.integration_method_id = m.id AND mp.deleted_at IS NULL
    WHERE p.deleted_at IS NULL AND p.vendor = ANY(@vendors::text[])
      AND (m.vendor IN ('Cross-platform', 'Others') OR p.vendor = m.vendor)
  ), false)::boolean AS not_applicable_everywhere
FROM onboarding_step_methods sm
JOIN onboarding_steps s ON s.id = sm.step_id AND s.deleted_at IS NULL
JOIN support_matrix_integration_methods m ON m.id = sm.integration_method_id AND m.deleted_at IS NULL
WHERE s.slug = ANY(@step_slugs::text[])
ORDER BY s.slug, m.sort_order, m.slug;
