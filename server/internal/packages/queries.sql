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

