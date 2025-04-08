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