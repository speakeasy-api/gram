-- name: PeekLatestPackageVersionByName :one
SELECT packages.id as package_id, package_versions.id as package_version_id
FROM packages
INNER JOIN package_versions ON packages.id = package_versions.package_id
WHERE packages.name = @name
ORDER BY package_versions.id DESC
LIMIT 1;

-- name: PeekPackageByNameAndVersion :one
SELECT packages.id as package_id, package_versions.id as package_version_id
FROM packages
INNER JOIN package_versions ON packages.id = package_versions.package_id
WHERE packages.name = @name
  AND package_versions.major = @major
  AND package_versions.minor = @minor
  AND package_versions.patch = @patch
  AND package_versions.prerelease = @prerelease
  AND package_versions.build = @build
LIMIT 1;

-- name: ListPackagesByVersionIDs :many
SELECT packages.id as package_id, packages.name as package_name, sqlc.embed(package_versions)
FROM package_versions
INNER JOIN packages ON package_versions.package_id = packages.id
WHERE package_versions.id = ANY(@ids::uuid[]);

-- name: PokePackageByName :one
SELECT id 
FROM packages
WHERE name = @name
  AND project_id = @project_id
LIMIT 1;

-- name: GetPackageWithLatestVersion :one
WITH package_id_lookup as (
  SELECT id
  FROM packages
  WHERE (
      (sqlc.narg(package_id)::UUID IS NOT NULL AND packages.id = sqlc.narg(package_id)::UUID)
      OR (sqlc.narg(package_name)::TEXT IS NOT NULL AND packages.name = sqlc.narg(package_name)::TEXT)
    )
    AND packages.project_id = @project_id
  LIMIT 1
),
latest_version as (
  SELECT
    id,
    package_id,
    deployment_id,
    major,
    minor,
    patch,
    prerelease,
    build,
    created_at
  FROM package_versions
  WHERE package_versions.package_id = (SELECT id FROM package_id_lookup)
    AND package_versions.visibility = 'public'
    AND package_versions.prerelease IS NULL
  ORDER BY major DESC, minor DESC, patch DESC
  LIMIT 1
)
SELECT
    sqlc.embed(packages)
  , latest_version.id as version_id
  , latest_version.deployment_id as version_deployment_id
  , latest_version.major as version_major
  , latest_version.minor as version_minor
  , latest_version.patch as version_patch
  , latest_version.prerelease as version_prerelease
  , latest_version.build as version_build
  , latest_version.created_at as version_created_at
FROM packages
LEFT JOIN latest_version ON packages.id = latest_version.package_id
WHERE packages.id = (SELECT id FROM package_id_lookup) AND packages.project_id = @project_id;

-- name: CreatePackage :one
INSERT INTO packages (
    name
  , title
  , summary
  , keywords
  , organization_id
  , project_id
)
VALUES (@name, @title, @summary, @keywords, @organization_id, @project_id)
RETURNING id;

-- name: ListVersions :many
WITH package_id_lookup as (
  SELECT id
  FROM packages
  WHERE (
      (sqlc.narg(package_id)::UUID IS NOT NULL AND packages.id = sqlc.narg(package_id)::UUID)
      OR (sqlc.narg(package_name)::TEXT IS NOT NULL AND packages.name = sqlc.narg(package_name)::TEXT)
    )
    AND packages.project_id = @project_id
  LIMIT 1
)
SELECT 
    sqlc.embed(packages)
  , pv.id as version_id
  , pv.deployment_id as version_deployment_id
  , pv.major as version_major
  , pv.minor as version_minor
  , pv.patch as version_patch
  , pv.prerelease as version_prerelease
  , pv.build as version_build
  , pv.visibility as version_visibility
  , pv.created_at as version_created_at
FROM package_versions as pv
INNER JOIN packages ON pv.package_id = packages.id
WHERE packages.id = (SELECT id FROM package_id_lookup) AND packages.project_id = @project_id;

-- name: CreatePackageVersion :one
INSERT INTO package_versions (
    package_id
  , deployment_id
  , major
  , minor
  , patch
  , prerelease
  , build
  , visibility
)
VALUES (@package_id, @deployment_id, @major, @minor, @patch, @prerelease, @build, @visibility)
RETURNING *;