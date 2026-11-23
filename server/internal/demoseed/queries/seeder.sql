-- name: Now :one
SELECT now()::timestamptz AS now;

-- name: PointAssetAtUpload :exec
UPDATE assets
SET url = @url, sha256 = @sha256, content_length = @content_length, content_type = 'application/x-yaml'
WHERE id = @id AND project_id = @project_id::uuid;

-- name: SetSeedStatementTimeout :exec
-- Transaction-local, so it reverts on commit or rollback and never leaks onto
-- a pooled connection.
SELECT set_config('statement_timeout', '360s', true);

-- name: TryAcquireSeedLock :one
-- Session-level, so it must run on the dedicated connection that later
-- releases it.
SELECT pg_try_advisory_lock(hashtext(@lock_name::text)) AS locked;

-- name: ReleaseSeedLock :one
SELECT pg_advisory_unlock(hashtext(@lock_name::text)) AS unlocked;
