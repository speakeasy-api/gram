-- name: GetOrganizationSyncLastEventID :one
SELECT last_event_id
FROM workos_organization_syncs
WHERE workos_organization_id = @workos_organization_id;

-- name: SetOrganizationSyncLastEventID :one
INSERT INTO workos_organization_syncs (workos_organization_id, last_event_id)
VALUES (@workos_organization_id, @last_event_id)
ON CONFLICT (workos_organization_id) DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = clock_timestamp()
RETURNING id;

-- name: GetUserSyncLastEventID :one
SELECT last_event_id
FROM workos_user_syncs
WHERE id = 1;

-- name: SetUserSyncLastEventID :one
INSERT INTO workos_user_syncs (id, last_event_id)
VALUES (1, @last_event_id)
ON CONFLICT (id) DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = clock_timestamp()
RETURNING id;
