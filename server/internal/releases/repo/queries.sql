-- name: CreateRelease :one
INSERT INTO toolset_releases (
    toolset_id
  , source_state_id
  , toolset_version_id
  , global_variations_version_id
  , toolset_variations_version_id
  , release_number
  , notes
  , released_by_user_id
) VALUES (
    @toolset_id
  , @source_state_id
  , @toolset_version_id
  , @global_variations_version_id
  , @toolset_variations_version_id
  , COALESCE(
      (SELECT MAX(release_number) + 1 FROM toolset_releases WHERE toolset_id = @toolset_id),
      1
    )
  , @notes
  , @released_by_user_id
)
RETURNING *;

-- name: GetRelease :one
SELECT *
FROM toolset_releases
WHERE id = @id;

-- name: GetReleaseByNumber :one
SELECT *
FROM toolset_releases
WHERE toolset_id = @toolset_id
  AND release_number = @release_number;

-- name: ListReleases :many
SELECT *
FROM toolset_releases
WHERE toolset_id = @toolset_id
ORDER BY release_number DESC
LIMIT @limit_count
OFFSET @offset_count;

-- name: GetLatestRelease :one
SELECT *
FROM toolset_releases
WHERE toolset_id = @toolset_id
ORDER BY release_number DESC
LIMIT 1;

-- name: CountReleases :one
SELECT COUNT(*)
FROM toolset_releases
WHERE toolset_id = @toolset_id;

-- name: DeleteRelease :exec
DELETE FROM toolset_releases
WHERE id = @id;

-- name: CreateSystemSourceState :one
INSERT INTO system_source_states (
    project_id
  , prompt_template_ids
) VALUES (
    @project_id
  , @prompt_template_ids
)
RETURNING *;

-- name: GetSystemSourceState :one
SELECT *
FROM system_source_states
WHERE id = @id;

-- name: CreateSourceState :one
INSERT INTO source_states (
    project_id
  , deployment_id
  , system_source_state_id
) VALUES (
    @project_id
  , @deployment_id
  , @system_source_state_id
)
RETURNING *;

-- name: GetSourceState :one
SELECT *
FROM source_states
WHERE id = @id;

-- name: GetSourceStateByComponents :one
SELECT *
FROM source_states
WHERE deployment_id = @deployment_id
  AND system_source_state_id = @system_source_state_id
LIMIT 1;

-- name: CreateToolVariationsGroupVersion :one
INSERT INTO tool_variations_group_versions (
    group_id
  , version
  , variation_ids
  , predecessor_id
) VALUES (
    @group_id
  , COALESCE(
      (SELECT MAX(version) + 1 FROM tool_variations_group_versions WHERE group_id = @group_id),
      1
    )
  , @variation_ids
  , @predecessor_id
)
RETURNING *;

-- name: GetToolVariationsGroupVersion :one
SELECT *
FROM tool_variations_group_versions
WHERE id = @id;

-- name: GetLatestToolVariationsGroupVersion :one
SELECT *
FROM tool_variations_group_versions
WHERE group_id = @group_id
ORDER BY version DESC
LIMIT 1;

-- name: ListToolVariationsGroupVersions :many
SELECT *
FROM tool_variations_group_versions
WHERE group_id = @group_id
ORDER BY version DESC;
