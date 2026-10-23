-- name: GetSettings :one
SELECT hidden, environment_id, version
FROM builtin_mcp_settings
WHERE project_id = @project_id AND builtin_key = @builtin_key;

-- name: InsertSettings :one
INSERT INTO builtin_mcp_settings (project_id, builtin_key, hidden, environment_id)
VALUES (@project_id, @builtin_key, @hidden, sqlc.narg(environment_id))
ON CONFLICT (project_id, builtin_key) DO NOTHING
RETURNING hidden, environment_id, version;

-- name: UpdateSettings :one
UPDATE builtin_mcp_settings
SET hidden = @hidden, environment_id = sqlc.narg(environment_id), version = version + 1, updated_at = clock_timestamp()
WHERE project_id = @project_id AND builtin_key = @builtin_key AND version = @expected_version
RETURNING hidden, environment_id, version;
