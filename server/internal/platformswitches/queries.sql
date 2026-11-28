-- name: GetPlatformSwitch :one
SELECT *
FROM platform_switches
WHERE key = @key;

-- name: SetPlatformSwitch :one
INSERT INTO platform_switches (key, engaged, updated_by)
VALUES (@key, @engaged, @updated_by)
ON CONFLICT (key) DO UPDATE
SET engaged = EXCLUDED.engaged,
    updated_by = EXCLUDED.updated_by,
    updated_at = clock_timestamp()
RETURNING *;
