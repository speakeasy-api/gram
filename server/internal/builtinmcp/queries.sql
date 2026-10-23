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
WHERE issuer.id = @id AND issuer.deleted IS FALSE
  AND (issuer.project_id IS NULL OR issuer.project_id = @project_id::uuid)
  AND COALESCE(issuer.organization_id, project.organization_id) = @organization_id::text
FOR SHARE OF issuer;
