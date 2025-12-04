-- name: CreateAsset :one
INSERT INTO assets (
    name
  , url
  , project_id
  , sha256
  , kind
  , content_type
  , content_length
) VALUES (
    @name
  , @url
  , @project_id
  , @sha256
  , @kind
  , @content_type
  , @content_length
)
ON CONFLICT (project_id, sha256) DO UPDATE SET
    deleted_at = NULL,
    url = @url,
    updated_at = clock_timestamp()
RETURNING *;

-- name: GetProjectAsset :one
SELECT * FROM assets WHERE project_id = @project_id AND id = @id;

-- name: GetProjectAssetBySHA256 :one
SELECT * FROM assets WHERE project_id = @project_id AND sha256 = @sha256;

-- name: GetImageAssetURL :one
SELECT url, content_type, content_length, updated_at FROM assets WHERE id = @id AND kind = 'image';

-- name: GetOpenAPIv3AssetURL :one
SELECT url, content_type, content_length, updated_at
FROM assets
WHERE
  id = @id AND kind = 'openapiv3'
  AND project_id = @project_id;

-- name: GetFunctionAssetURL :one
SELECT url, content_type, content_length, updated_at
FROM assets
WHERE
  id = @id AND kind = 'functions'
  AND project_id = @project_id;

-- name: ListAssets :many
SELECT * FROM assets WHERE project_id = @project_id;

-- name: GetAssetsByID :many
SELECT id, url, sha256, content_type, content_length
FROM assets
WHERE project_id = @project_id
  AND id = ANY(@ids::uuid[])
  AND deleted IS FALSE;