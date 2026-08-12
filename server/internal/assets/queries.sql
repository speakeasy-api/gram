-- name: CreateAsset :one
-- Project-tier upsert. A project-scoped row carries both project_id and
-- organization_id; the DO UPDATE re-asserts organization_id so resurrected or
-- re-uploaded rows created before the dual-write get repaired on conflict.
INSERT INTO assets (
    name
  , url
  , project_id
  , organization_id
  , sha256
  , kind
  , content_type
  , content_length
) VALUES (
    @name
  , @url
  , @project_id::uuid
  , @organization_id::text
  , @sha256
  , @kind
  , @content_type
  , @content_length
)
ON CONFLICT (project_id, sha256) DO UPDATE SET
    deleted_at = NULL,
    url = @url,
    organization_id = EXCLUDED.organization_id,
    updated_at = clock_timestamp()
RETURNING *;

-- name: CreateOrganizationAsset :one
-- Organization-tier upsert (project_id IS NULL, organization_id set). The ON
-- CONFLICT target repeats the assets_organization_id_sha256_key partial index
-- predicate verbatim: Postgres refuses to infer a partial index from a
-- mismatched predicate, and a conflict against a non-arbiter index would
-- surface as an uncaught 23505 on the second upload of the same bytes.
INSERT INTO assets (
    name
  , url
  , organization_id
  , sha256
  , kind
  , content_type
  , content_length
) VALUES (
    @name
  , @url
  , @organization_id::text
  , @sha256
  , @kind
  , @content_type
  , @content_length
)
ON CONFLICT (organization_id, sha256)
  WHERE project_id IS NULL AND organization_id IS NOT NULL
DO UPDATE SET
    deleted_at = NULL,
    url = @url,
    updated_at = clock_timestamp()
RETURNING *;

-- name: CreatePlatformAsset :one
-- Platform-tier upsert (project_id IS NULL, organization_id IS NULL). The ON
-- CONFLICT target repeats the assets_platform_sha256_key partial index
-- predicate verbatim, for the same arbiter-inference reason as the
-- organization-tier upsert.
INSERT INTO assets (
    name
  , url
  , sha256
  , kind
  , content_type
  , content_length
) VALUES (
    @name
  , @url
  , @sha256
  , @kind
  , @content_type
  , @content_length
)
ON CONFLICT (sha256)
  WHERE project_id IS NULL AND organization_id IS NULL
DO UPDATE SET
    deleted_at = NULL,
    url = @url,
    updated_at = clock_timestamp()
RETURNING *;

-- name: SeedProjectAssetWithoutOrganization :one
-- Test fixture only: simulates a project-tier row created before the
-- organization_id dual-write shipped. Application code must use CreateAsset,
-- which always sets organization_id.
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
  , @project_id::uuid
  , @sha256
  , @kind
  , @content_type
  , @content_length
)
RETURNING *;

-- name: GetProjectAsset :one
SELECT * FROM assets WHERE project_id = @project_id::uuid AND id = @id;

-- name: GetProjectAssetBySHA256 :one
SELECT * FROM assets WHERE project_id = @project_id::uuid AND sha256 = @sha256;

-- name: GetOrganizationAssetBySHA256 :one
SELECT * FROM assets
WHERE organization_id = @organization_id::text
  AND project_id IS NULL
  AND sha256 = @sha256;

-- name: GetPlatformAssetBySHA256 :one
SELECT * FROM assets
WHERE project_id IS NULL
  AND organization_id IS NULL
  AND sha256 = @sha256;

-- name: GetImageAssetURL :one
SELECT url, content_type, content_length, updated_at FROM assets WHERE id = @id AND kind = 'image';

-- name: GetOpenAPIv3AssetURL :one
SELECT url, content_type, content_length, updated_at
FROM assets
WHERE
  id = @id AND kind = 'openapiv3'
  AND project_id = @project_id::uuid;

-- name: GetFunctionAssetURL :one
SELECT url, content_type, content_length, updated_at
FROM assets
WHERE
  id = @id AND kind = 'functions'
  AND project_id = @project_id::uuid;

-- name: GetChatAttachmentAssetURL :one
SELECT url, content_type, content_length, updated_at
FROM assets
WHERE
  id = @id AND kind = 'chat_attachment'
  AND project_id = @project_id::uuid
  AND deleted = false;

-- name: ListAssets :many
SELECT * FROM assets WHERE project_id = @project_id::uuid;

-- name: GetAssetsByID :many
SELECT id, url, sha256, content_type, content_length
FROM assets
WHERE project_id = @project_id::uuid
  AND id = ANY(@ids::uuid[])
  AND deleted IS FALSE;