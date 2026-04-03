-- name: OnboardWorkOSOrganization :one
INSERT INTO organization_metadata (
    id
  , workos_id
  , name
  , slug
)
VALUES (
    @speakeasy_id
  , @workos_id
  , @name
  , @slug
)
ON CONFLICT (id) DO UPDATE
SET
    workos_id = EXCLUDED.workos_id
  , name = EXCLUDED.name
  , slug = COALESCE(organization_metadata.slug, EXCLUDED.slug)
  , updated_at = clock_timestamp()
  , deleted_at = NULL
RETURNING id;

-- name: GetOrganizationSyncLastEventID :one
SELECT last_event_id
FROM workos_organization_syncs
WHERE workos_organization_id = @workos_organization_id;

-- name: SetOrganizationSyncLastEventID :one
INSERT INTO workos_organization_syncs (workos_organization_id, last_event_id)
VALUES (@workos_organization_id, @last_event_id)
RETURNING id;

-- name: SetUserSyncLastEventID :one
INSERT INTO workos_user_syncs (last_event_id)
VALUES (@last_event_id)
RETURNING id;
