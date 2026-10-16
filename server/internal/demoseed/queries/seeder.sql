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
