-- Bindings are pointers only: assistant -> dedicated agent, and root trigger ->
-- (workload issuer, subject). Everything they point at is owned and edited
-- through the ordinary agent and workload identity surfaces.

-- name: LockAssistant :one
SELECT id, organization_id, name, created_by_user_id
FROM assistants
WHERE project_id = @project_id AND id = @assistant_id AND deleted IS FALSE
FOR UPDATE;

-- Conflicts with LockAssistant, so a trigger bind and an upgrade of the same
-- assistant serialize; concurrent trigger binds do not block each other.
-- name: ShareLockAssistant :one
SELECT id
FROM assistants
WHERE project_id = @project_id AND id = @assistant_id AND deleted IS FALSE
FOR SHARE;

-- Agent liveness mirrors agents/lifecycle.Derive: an agent is active unless it
-- is deleted, revoked, or suspended.
-- name: ListAssistantAgentStates :many
SELECT b.original_assistant_id AS assistant_id,
       b.original_agent_id AS agent_id,
       (g.id IS NOT NULL AND g.deleted IS FALSE AND g.revoked_at IS NULL AND g.suspended_at IS NULL)::boolean AS agent_active
FROM assistant_agent_bindings b
LEFT JOIN agents g ON g.organization_id = b.organization_id AND g.id = b.agent_id
WHERE b.project_id = @project_id
  AND b.original_assistant_id = ANY(@assistant_ids::uuid[])
  AND b.deleted IS FALSE;

-- name: GetAssistantBinding :one
SELECT id, organization_id, original_agent_id, generation
FROM assistant_agent_bindings
WHERE project_id = @project_id AND original_assistant_id = @assistant_id AND deleted IS FALSE;

-- name: CreateAssistantBinding :one
INSERT INTO assistant_agent_bindings (
  organization_id, project_id, project_ref_organization_id, project_ref_id,
  original_assistant_id, assistant_ref_organization_id, assistant_ref_project_id, assistant_id,
  original_agent_id, agent_ref_organization_id, agent_ref_project_id, agent_id, generation
)
SELECT @organization_id::text, @project_id::uuid, @organization_id::text, @project_id::uuid,
  @assistant_id::uuid, @organization_id::text, @project_id::uuid, @assistant_id::uuid,
  @agent_id::uuid, @organization_id::text, @project_id::uuid, @agent_id::uuid,
  COALESCE(MAX(generation), 0) + 1
FROM assistant_agent_bindings
WHERE original_assistant_id = @assistant_id::uuid
RETURNING id, generation;

-- name: TombstoneAssistantBinding :exec
UPDATE assistant_agent_bindings
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE project_id = @project_id AND original_assistant_id = @assistant_id AND deleted IS FALSE;

-- name: ListAssistantRoots :many
SELECT id
FROM trigger_instances
WHERE project_id = @project_id
  AND target_kind = 'assistant' AND target_ref = @assistant_id::text
  AND definition_slug <> 'wake' AND deleted IS FALSE
ORDER BY id
FOR UPDATE;

-- name: GetTriggerBinding :one
SELECT id, organization_id, original_workload_issuer_id AS workload_issuer_id, subject
FROM trigger_workload_bindings
WHERE project_id = @project_id AND original_trigger_id = @trigger_id AND deleted IS FALSE;

-- name: GetAssistantTriggerBinding :one
SELECT tb.organization_id, tb.original_workload_issuer_id AS workload_issuer_id, tb.subject
FROM trigger_workload_bindings tb
JOIN assistant_agent_bindings ab ON ab.organization_id = tb.assistant_binding_ref_organization_id
  AND ab.project_id = tb.assistant_binding_ref_project_id AND ab.id = tb.assistant_binding_id
WHERE tb.project_id = @project_id AND tb.original_trigger_id = @trigger_id AND tb.deleted IS FALSE
  AND ab.original_assistant_id = @assistant_id AND ab.deleted IS FALSE;

-- name: ListAssistantTriggerBindings :many
SELECT tb.original_trigger_id
FROM trigger_workload_bindings tb
JOIN assistant_agent_bindings ab ON ab.organization_id = tb.assistant_binding_ref_organization_id
  AND ab.project_id = tb.assistant_binding_ref_project_id AND ab.id = tb.assistant_binding_id
WHERE ab.project_id = @project_id AND ab.original_assistant_id = @assistant_id
  AND ab.deleted IS FALSE AND tb.deleted IS FALSE
ORDER BY tb.original_trigger_id;

-- name: LockTriggers :many
SELECT id FROM trigger_instances
WHERE project_id = @project_id AND id = ANY(@trigger_ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: CreateTriggerBinding :exec
INSERT INTO trigger_workload_bindings (
  organization_id, project_id, project_ref_organization_id, project_ref_id,
  original_trigger_id, trigger_ref_organization_id, trigger_ref_project_id, trigger_id,
  original_assistant_binding_id, assistant_binding_ref_organization_id, assistant_binding_ref_project_id, assistant_binding_id,
  assistant_binding_generation, original_workload_issuer_id, workload_issuer_ref_organization_id,
  workload_issuer_ref_project_id, workload_issuer_id, subject, generation
)
SELECT @organization_id::text, @project_id::uuid, @organization_id::text, @project_id::uuid,
  @trigger_id::uuid, @organization_id::text, @project_id::uuid, @trigger_id::uuid,
  @assistant_binding_id::uuid, @organization_id::text, @project_id::uuid, @assistant_binding_id::uuid,
  @assistant_binding_generation::bigint, @workload_issuer_id::uuid, @organization_id::text, @project_id::uuid, @workload_issuer_id::uuid,
  @subject::text, COALESCE(MAX(generation), 0) + 1
FROM trigger_workload_bindings
WHERE original_trigger_id = @trigger_id::uuid;

-- name: TombstoneTriggerBinding :exec
UPDATE trigger_workload_bindings
SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE project_id = @project_id AND original_trigger_id = @trigger_id AND deleted IS FALSE;

-- name: FindProjectIssuer :many
SELECT id
FROM workload_issuers
WHERE organization_id = @organization_id AND project_id = @project_id
  AND issuer = ANY(@issuers::text[]) AND deleted IS FALSE
ORDER BY created_at, id;

-- A concurrent binder in the same project may win the name; the caller then
-- re-reads the winner by issuer URL.
-- name: CreateProjectIssuer :one
INSERT INTO workload_issuers (organization_id, project_id, name, issuer, jwks_uri, allow_wildcard_admission)
VALUES (@organization_id, @project_id, @name, @issuer, @jwks_uri, false)
ON CONFLICT (project_id, name) WHERE deleted IS FALSE DO NOTHING
RETURNING id;

-- name: ListExactAdmissions :many
SELECT id
FROM workload_identity_admissions
WHERE organization_id = @organization_id AND project_id = @project_id
  AND workload_issuer_id = @workload_issuer_id AND match_kind = 'exact' AND subject = @subject
  AND deleted IS FALSE
ORDER BY id;
