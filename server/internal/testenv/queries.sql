-- name: InsertChatMessage :one
INSERT INTO chat_messages (chat_id, project_id, role, content)
VALUES (@chat_id, @project_id, @role, @content)
RETURNING id;

-- name: ListDeploymentHTTPTools :many
SELECT *
FROM http_tool_definitions
WHERE deployment_id = @deployment_id;

-- name: ListDeploymentFunctionsTools :many
SELECT *
FROM function_tool_definitions
WHERE deployment_id = @deployment_id;

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
