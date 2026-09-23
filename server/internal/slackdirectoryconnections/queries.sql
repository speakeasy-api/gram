-- name: ListSlackDirectoryConnections :many
SELECT * FROM slack_directory_connections
WHERE organization_id = @organization_id
ORDER BY created_at, id;

-- name: GetSlackDirectoryConnection :one
SELECT * FROM slack_directory_connections
WHERE organization_id = @organization_id AND id = @id;

-- name: LockSlackDirectoryConnection :one
SELECT * FROM slack_directory_connections
WHERE organization_id = @organization_id AND id = @id
FOR UPDATE;

-- name: LockSlackDirectoryConnectionByTeam :one
SELECT * FROM slack_directory_connections
WHERE organization_id = @organization_id AND slack_team_id = @slack_team_id
FOR UPDATE;

-- name: CreateSlackDirectoryConnection :one
INSERT INTO slack_directory_connections (organization_id, slack_team_id, slack_team_name, credentials_encrypted, granted_scopes, generation, health)
VALUES (@organization_id, @slack_team_id, @slack_team_name, @credentials_encrypted, @granted_scopes, @generation, 'connected')
ON CONFLICT (organization_id, slack_team_id) DO NOTHING
RETURNING *;

-- name: AuthorizeSlackDirectoryConnection :one
UPDATE slack_directory_connections SET
slack_team_name = @slack_team_name,
credentials_encrypted = @credentials_encrypted,
granted_scopes = @granted_scopes,
generation = @generation,
health = 'connected', disconnected_at = NULL, last_error_code = NULL,
updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND id = @id AND generation = @expected_generation AND slack_team_id = @slack_team_id
RETURNING *;

-- name: DisconnectSlackDirectoryConnection :one
UPDATE slack_directory_connections SET
credentials_encrypted = NULL, granted_scopes = '{}', generation = @generation,
health = 'disconnected', disconnected_at = clock_timestamp(), updated_at = clock_timestamp(), last_error_code = NULL
WHERE organization_id = @organization_id AND id = @id AND generation = @expected_generation
RETURNING *;
