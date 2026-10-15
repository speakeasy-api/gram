-- name: Now :one
SELECT now()::timestamptz AS now;

-- name: PointAssetAtUpload :exec
UPDATE assets
SET url = @url, sha256 = @sha256, content_length = @content_length, content_type = 'application/x-yaml'
WHERE id = @id AND project_id = @project_id::uuid;
