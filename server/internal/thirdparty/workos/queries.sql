-- name: GetOrganizationSyncLastEventID :one
SELECT last_event_id
FROM workos_organization_syncs
WHERE workos_organization_id = @workos_organization_id;

-- name: SetOrganizationSyncLastEventID :one
INSERT INTO workos_organization_syncs (workos_organization_id, last_event_id)
VALUES (@workos_organization_id, @last_event_id)
ON CONFLICT (workos_organization_id) DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    created_at = clock_timestamp()
RETURNING id;

-- name: SetUserSyncLastEventID :one
INSERT INTO workos_user_syncs (last_event_id)
VALUES (@last_event_id)
RETURNING id;
