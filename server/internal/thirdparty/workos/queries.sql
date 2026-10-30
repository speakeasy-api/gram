-- name: LockOrganizationSync :exec
SELECT pg_advisory_xact_lock(hashtextextended('workos-organization-sync:' || @workos_organization_id::text, 0));

-- name: GetOrganizationSyncState :one
SELECT id, last_event_id, updated_at
FROM workos_organization_syncs
WHERE workos_organization_id = @workos_organization_id;

-- name: GetOrganizationSyncLastEventID :one
SELECT last_event_id
FROM workos_organization_syncs
WHERE workos_organization_id = @workos_organization_id;

-- name: SetOrganizationSyncLastEventID :one
INSERT INTO workos_organization_syncs (workos_organization_id, last_event_id)
VALUES (@workos_organization_id, @last_event_id)
ON CONFLICT (workos_organization_id) DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = GREATEST(clock_timestamp(), workos_organization_syncs.updated_at + interval '1 microsecond')
RETURNING id;

-- name: GetUserSyncLastEventID :one
SELECT last_event_id
FROM workos_user_syncs
WHERE workos_user_id = @workos_user_id;

-- name: SetUserSyncLastEventID :one
INSERT INTO workos_user_syncs (workos_user_id, last_event_id)
VALUES (@workos_user_id, @last_event_id)
ON CONFLICT (workos_user_id) WHERE workos_user_id IS NOT NULL DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = clock_timestamp()
RETURNING id;
