-- name: InsertChatMessage :one
INSERT INTO chat_messages (chat_id, project_id, role, content)
VALUES (@chat_id, @project_id, @role, @content)
RETURNING id;

-- name: ForceSoftDeleteChat :exec
-- Bypasses the production SoftDeleteChat guard (which refuses to delete a chat
-- backing a live assistant thread) so tests can wedge the database into the
-- legacy/abnormal state that the runtime's self-heal exists to recover from.
UPDATE chats
SET deleted_at = clock_timestamp()
WHERE id = @id;

-- name: UpdateChatMessageCreatedAt :exec
UPDATE chat_messages
SET created_at = @created_at
WHERE id = @id;

-- name: UpdateRiskResultCreatedAt :exec
UPDATE risk_results
SET created_at = @created_at
WHERE id = @id;

-- name: ListDeploymentHTTPTools :many
SELECT *
FROM http_tool_definitions
WHERE deployment_id = @deployment_id;

-- name: ListDeploymentFunctionsTools :many
SELECT *
FROM function_tool_definitions
WHERE deployment_id = @deployment_id;

-- name: SetFunctionToolVariables :exec
UPDATE function_tool_definitions
SET variables = @variables
WHERE id = @id
  AND project_id = @project_id;

-- name: CountFunctionsAccess :one
SELECT count(id)
FROM functions_access
WHERE
  project_id = @project_id
  AND deployment_id = @deployment_id;

-- name: ListDeploymentFunctionsResources :many
SELECT *
FROM function_resource_definitions
WHERE deployment_id = @deployment_id;

-- name: ScrubDeploymentFunctionMachineSpecs :exec
-- Simulates a legacy deployment by NULLing out memory_mib and scale, as if the row was inserted before these columns existed.
UPDATE deployments_functions SET memory_mib = NULL, scale = NULL WHERE deployment_id = @deployment_id;

-- name: SetDeploymentFunctionInfraOverrides :exec
UPDATE deployments_functions SET memory_mib_override = @memory_mib_override, scale_override = @scale_override WHERE deployment_id = @deployment_id;

-- name: GetDeploymentFunctionInfraOverrides :many
SELECT memory_mib_override, scale_override FROM deployments_functions WHERE deployment_id = @deployment_id;
-- name: CountOutboxEntriesByEventType :one
SELECT COUNT(*)
FROM outbox
WHERE event_type = @event_type;

-- name: ListRiskResultsAll :many
-- Fixture query used by the risk-analysis activity tests that need to
-- inspect dead-letter and "no findings" rows the production queries filter
-- out via `found IS TRUE`.
SELECT *
FROM risk_results
WHERE project_id = @project_id
  AND risk_policy_id = @risk_policy_id
ORDER BY id;

-- name: GetOutboxEntry :one
-- Returns the ID of an outbox row; errors with pgx.ErrNoRows if deleted.
SELECT id FROM outbox WHERE id = @id;

-- name: GetOutboxRelayState :one
-- Reads the relay tracking state for a single outbox row.
SELECT
    outbox_id,
    processed_at,
    noop,
    dead_lettered,
    svix_message_id,
    attempts,
    last_error
FROM outbox_relays
WHERE outbox_id = @outbox_id;

-- name: SetOrgWebhookConfig :exec
-- Sets the Svix app ID and webhooks_enabled flag on an organization.
UPDATE organization_metadata
SET svix_app_id = @svix_app_id,
    webhooks_enabled = @webhooks_enabled,
    updated_at = clock_timestamp()
WHERE id = @organization_id;

-- name: CreateOrganizationMetadataFixture :exec
-- Test-only fixture that lets seeders populate every column on
-- organization_metadata. Prefer this over CreateOrganizationMetadata when a
-- test needs to exercise filters that depend on account type, workos linkage,
-- disabled state, whitelist flag, or trial window.
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    gram_account_type,
    workos_id,
    whitelisted,
    free_trial_started_at,
    free_trial_ends_at,
    disabled_at
) VALUES (
    @id,
    @name,
    @slug,
    @gram_account_type,
    sqlc.narg('workos_id')::text,
    @whitelisted,
    @free_trial_started_at,
    @free_trial_ends_at,
    sqlc.narg('disabled_at')::timestamptz
);

-- name: CreateOrganizationUserRelationshipFixture :exec
-- Test-only fixture for seeding membership counts.
INSERT INTO organization_user_relationships (organization_id, user_id)
VALUES (@organization_id, sqlc.narg('user_id')::text);

-- name: ForceSoftDeleteUserSessionIssuer :exec
-- Test-only fixture for defensive paths that handle a dangling soft-delete FK.
UPDATE user_session_issuers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;
