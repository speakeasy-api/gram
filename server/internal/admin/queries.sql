-- name: GetProjectByID :one
SELECT id, slug
FROM projects
WHERE id = @id
  AND deleted IS FALSE;

-- name: GetProjectBySlug :one
SELECT id, slug
FROM projects
WHERE slug = @slug
  AND deleted IS FALSE;

-- name: AdminGetProjectDetailByID :one
SELECT
    p.id,
    p.name,
    p.slug,
    p.organization_id,
    p.logo_asset_id,
    p.functions_runner_version,
    p.created_at,
    p.updated_at,
    (SELECT count(*) FROM toolsets t WHERE t.project_id = p.id AND t.deleted IS FALSE)::bigint AS toolset_count,
    (SELECT count(*) FROM deployments d WHERE d.project_id = p.id)::bigint AS deployment_count,
    (SELECT count(*) FROM http_tool_definitions h WHERE h.project_id = p.id AND h.deleted IS FALSE)::bigint AS http_tool_count,
    (SELECT count(*) FROM environments e WHERE e.project_id = p.id AND e.deleted IS FALSE)::bigint AS environment_count,
    (SELECT count(*) FROM api_keys k WHERE k.project_id = p.id AND k.deleted IS FALSE)::bigint AS api_key_count,
    (SELECT count(*) FROM assistants a WHERE a.project_id = p.id AND a.deleted IS FALSE)::bigint AS assistant_count
FROM projects p
WHERE p.id = @id
  AND p.deleted IS FALSE;

-- name: AdminGetProjectDetailBySlug :one
SELECT
    p.id,
    p.name,
    p.slug,
    p.organization_id,
    p.logo_asset_id,
    p.functions_runner_version,
    p.created_at,
    p.updated_at,
    (SELECT count(*) FROM toolsets t WHERE t.project_id = p.id AND t.deleted IS FALSE)::bigint AS toolset_count,
    (SELECT count(*) FROM deployments d WHERE d.project_id = p.id)::bigint AS deployment_count,
    (SELECT count(*) FROM http_tool_definitions h WHERE h.project_id = p.id AND h.deleted IS FALSE)::bigint AS http_tool_count,
    (SELECT count(*) FROM environments e WHERE e.project_id = p.id AND e.deleted IS FALSE)::bigint AS environment_count,
    (SELECT count(*) FROM api_keys k WHERE k.project_id = p.id AND k.deleted IS FALSE)::bigint AS api_key_count,
    (SELECT count(*) FROM assistants a WHERE a.project_id = p.id AND a.deleted IS FALSE)::bigint AS assistant_count
FROM projects p
WHERE p.slug = @slug
  AND p.deleted IS FALSE;

-- name: AdminListOrganizations :many
SELECT
    om.id,
    om.name,
    om.slug,
    om.gram_account_type AS account_type,
    om.workos_id,
    om.whitelisted,
    om.disabled_at,
    om.free_trial_started_at,
    om.free_trial_ends_at,
    om.created_at,
    om.updated_at,
    (
        SELECT count(*)
        FROM organization_user_relationships our
        WHERE our.organization_id = om.id
          AND our.deleted IS FALSE
    )::bigint AS member_count
FROM organization_metadata om
WHERE
    (sqlc.narg('q')::text IS NULL OR om.name ILIKE '%' || sqlc.narg('q')::text || '%' OR om.slug ILIKE '%' || sqlc.narg('q')::text || '%')
    AND (sqlc.narg('account_type')::text IS NULL OR om.gram_account_type = sqlc.narg('account_type')::text)
    AND (sqlc.arg('include_disabled')::boolean OR om.disabled_at IS NULL)
    AND (sqlc.narg('after_id')::text IS NULL OR om.id > sqlc.narg('after_id')::text)
ORDER BY om.id ASC
LIMIT sqlc.arg('page_limit')::int;

-- name: AdminUpdateOrganization :exec
-- Admin-only mutation. Both fields are optional — caller passes NULL to skip
-- the field. NULL on both is a no-op (still touches updated_at).
UPDATE organization_metadata
SET
    gram_account_type = COALESCE(sqlc.narg('account_type')::text, gram_account_type),
    whitelisted = COALESCE(sqlc.narg('whitelisted')::boolean, whitelisted),
    updated_at = clock_timestamp()
WHERE id = @id;

-- name: AdminListProjectsForOrganization :many
SELECT id, slug, name, created_at, updated_at
FROM projects
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY created_at DESC
LIMIT 200;

-- name: AdminGetOrganizationByIDOrSlug :one
SELECT
    om.id,
    om.name,
    om.slug,
    om.gram_account_type AS account_type,
    om.workos_id,
    om.whitelisted,
    om.disabled_at,
    om.free_trial_started_at,
    om.free_trial_ends_at,
    om.created_at,
    om.updated_at,
    (
        SELECT count(*)
        FROM organization_user_relationships our
        WHERE our.organization_id = om.id
          AND our.deleted IS FALSE
    )::bigint AS member_count
FROM organization_metadata om
WHERE om.id = sqlc.arg('id_or_slug')::text
   OR om.slug = sqlc.arg('id_or_slug')::text
LIMIT 1;
