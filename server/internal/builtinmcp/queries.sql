-- name: GetSettings :one
SELECT hidden, environment_id, user_session_issuer_id, user_session_issuer_bound, version
FROM builtin_mcp_settings
WHERE project_id = @project_id AND builtin_key = @builtin_key;

-- name: InsertSettings :one
INSERT INTO builtin_mcp_settings (project_id, builtin_key, hidden, environment_id, user_session_issuer_id, user_session_issuer_bound)
VALUES (@project_id, @builtin_key, @hidden, sqlc.narg(environment_id), sqlc.narg(user_session_issuer_id), @user_session_issuer_bound)
ON CONFLICT (project_id, builtin_key) DO NOTHING
RETURNING hidden, environment_id, user_session_issuer_id, user_session_issuer_bound, version;

-- name: UpdateSettings :one
UPDATE builtin_mcp_settings
SET hidden = @hidden, environment_id = sqlc.narg(environment_id), user_session_issuer_id = sqlc.narg(user_session_issuer_id), user_session_issuer_bound = @user_session_issuer_bound, version = version + 1, updated_at = clock_timestamp()
WHERE project_id = @project_id AND builtin_key = @builtin_key AND version = @expected_version
RETURNING hidden, environment_id, user_session_issuer_id, user_session_issuer_bound, version;

-- name: BindUserSessionIssuer :one
INSERT INTO builtin_mcp_settings (project_id, builtin_key, user_session_issuer_id, user_session_issuer_bound)
VALUES (@project_id, @builtin_key, sqlc.narg(user_session_issuer_id), true)
ON CONFLICT (project_id, builtin_key) DO UPDATE
SET user_session_issuer_id = EXCLUDED.user_session_issuer_id,
    user_session_issuer_bound = true,
    version = builtin_mcp_settings.version + 1,
    updated_at = clock_timestamp()
WHERE NOT builtin_mcp_settings.user_session_issuer_bound
RETURNING id, hidden, environment_id, user_session_issuer_id, user_session_issuer_bound, version;

-- name: ValidateUserSessionIssuer :one
SELECT issuer.id
FROM user_session_issuers AS issuer
LEFT JOIN projects AS project ON project.id = issuer.project_id
JOIN projects AS target_project ON target_project.id = @project_id::uuid
  AND target_project.organization_id = @organization_id::text
  AND target_project.deleted IS FALSE
WHERE issuer.id = @id AND issuer.deleted IS FALSE
  AND (issuer.project_id IS NULL OR issuer.project_id = @project_id::uuid)
  AND COALESCE(issuer.organization_id, project.organization_id) = @organization_id::text
FOR SHARE OF issuer, target_project;

-- name: EnsureBuiltinServer :exec
INSERT INTO mcp_servers (id, project_id, name, builtin_backend, visibility, requires_upstream_auth, user_session_issuer_id)
SELECT @id::uuid, p.id, @name::text, @builtin_key::text, 'private', true,
  CASE WHEN settings.user_session_issuer_bound THEN settings.user_session_issuer_id
       ELSE sqlc.narg(user_session_issuer_id)::uuid END
FROM projects p
LEFT JOIN builtin_mcp_settings settings ON settings.project_id = p.id AND settings.builtin_key = @builtin_key::text
WHERE p.id = @project_id::uuid AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
ON CONFLICT (id) DO NOTHING;

-- name: SyncBuiltinServerIssuer :one
WITH target AS MATERIALIZED (
  SELECT ms.id, CASE
    WHEN settings.user_session_issuer_bound THEN settings.user_session_issuer_id
    ELSE COALESCE(sqlc.narg(user_session_issuer_id)::uuid, ms.user_session_issuer_id)
  END AS issuer_id
  FROM mcp_servers ms
  JOIN projects p ON p.id = ms.project_id
  LEFT JOIN builtin_mcp_settings settings ON settings.project_id = ms.project_id AND settings.builtin_key = ms.builtin_backend
  WHERE ms.id = @id::uuid AND ms.project_id = @project_id::uuid
    AND p.organization_id = @organization_id::text AND p.deleted IS FALSE
    AND ms.builtin_backend = @builtin_key::text AND ms.deleted IS FALSE
  FOR UPDATE OF ms
), updated AS (
  UPDATE mcp_servers ms SET user_session_issuer_id = target.issuer_id, updated_at = clock_timestamp()
  FROM target
  WHERE ms.id = target.id AND ms.project_id = @project_id::uuid
    AND ms.user_session_issuer_id IS DISTINCT FROM target.issuer_id
  RETURNING ms.id
)
SELECT id FROM target;

-- name: CreateBuiltinServerFixture :exec
INSERT INTO mcp_servers (id, project_id, builtin_backend, visibility)
VALUES (@id::uuid, @project_id::uuid, @builtin_backend::text, 'private');

-- name: GetBuiltinServerFixture :one
SELECT id, name, visibility, requires_upstream_auth, deleted
FROM mcp_servers
WHERE id = @id::uuid AND project_id = @project_id::uuid;
