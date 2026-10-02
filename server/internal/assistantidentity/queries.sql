-- name: LockAssistant :one
SELECT id FROM assistants WHERE organization_id = @organization_id AND project_id = @project_id AND id = @assistant_id FOR UPDATE;

-- name: GetAssistant :one
SELECT a.id, a.created_by_user_id, a.status, a.deleted,
       (p.deleted IS FALSE)::boolean AS project_live
FROM assistants a JOIN projects p ON p.id = a.project_id AND p.organization_id = a.organization_id
WHERE a.organization_id = @organization_id AND a.project_id = @project_id AND a.id = @assistant_id;

-- name: ListAssistantRoots :many
SELECT id FROM trigger_instances
WHERE organization_id = @organization_id AND project_id = @project_id
 AND target_kind = 'assistant' AND target_ref = @assistant_id::text
 AND NOT deleted AND status = 'active' AND definition_slug <> 'wake'
ORDER BY id;

-- name: RevokeDedicatedAdmissions :exec
UPDATE workload_identity_admissions adm SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE adm.organization_id = @organization_id AND adm.project_id = @project_id AND NOT adm.deleted
 AND EXISTS (SELECT 1 FROM workload_agent_assignments wa
 WHERE wa.organization_id = adm.organization_id AND wa.workload_issuer_id = adm.workload_issuer_id
 AND wa.subject = adm.subject AND wa.match_kind = adm.match_kind AND wa.agent_id = @agent_id AND NOT wa.deleted);

-- name: RevokeDedicatedAssignments :exec
UPDATE workload_agent_assignments wa SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE wa.organization_id = @organization_id AND wa.agent_id = @agent_id AND NOT wa.deleted
 AND NOT EXISTS (SELECT 1 FROM workload_identity_admissions adm WHERE adm.organization_id = wa.organization_id
 AND adm.workload_issuer_id = wa.workload_issuer_id AND adm.subject = wa.subject AND adm.match_kind = wa.match_kind AND NOT adm.deleted);

-- name: GetTrigger :one
SELECT id, definition_slug, target_kind, target_ref, status, deleted
FROM trigger_instances
WHERE organization_id = @organization_id AND project_id = @project_id AND id = @trigger_id;

-- name: LockActor :one
SELECT u.id FROM users u
JOIN organization_user_relationships m ON m.user_id = u.id
WHERE m.organization_id = @organization_id AND u.id = @user_id
  AND u.deleted_at IS NULL AND u.workos_deleted_at IS NULL AND m.deleted IS FALSE
FOR SHARE OF u, m;

-- name: GetAssistantBinding :one
SELECT b.id, b.original_assistant_id, b.original_agent_id, b.generation, b.deleted,
  (b.deleted OR b.project_ref_id IS NULL OR b.assistant_id IS NULL OR b.agent_id IS NULL OR g.deleted OR g.revoked_at IS NOT NULL OR g.owner_reassignment_required_at IS NOT NULL OR (a.created_by_user_id IS NOT NULL AND g.owner_user_id IS DISTINCT FROM a.created_by_user_id))::boolean AS tombstoned,
  COALESCE(NOT b.deleted AND b.project_ref_id IS NOT NULL AND b.assistant_id IS NOT NULL
    AND b.agent_id = b.original_agent_id AND b.assistant_id = b.original_assistant_id
    AND b.project_ref_id = b.project_id AND NOT p.deleted AND NOT a.deleted
    AND NOT g.deleted AND g.suspended_at IS NULL AND g.revoked_at IS NULL
    AND g.owner_reassignment_required_at IS NULL
    AND (a.created_by_user_id IS NULL OR g.owner_user_id = a.created_by_user_id) AND u.deleted_at IS NULL
    AND u.workos_deleted_at IS NULL AND NOT m.deleted AND m.user_id IS NOT NULL, false)::boolean AS eligible
FROM assistant_agent_bindings b
LEFT JOIN projects p ON p.organization_id = b.organization_id AND p.id = b.project_ref_id
LEFT JOIN assistants a ON a.organization_id = b.organization_id AND a.project_id = b.project_id AND a.id = b.assistant_id
LEFT JOIN agents g ON g.organization_id = b.organization_id AND g.project_id = b.project_id AND g.id = b.agent_id
LEFT JOIN users u ON u.id = g.owner_user_id
LEFT JOIN organization_user_relationships m ON m.organization_id = b.organization_id AND m.user_id = u.id
WHERE b.organization_id = @organization_id AND b.project_id = @project_id AND b.original_assistant_id = @assistant_id
ORDER BY b.generation DESC LIMIT 1;

-- name: GetTriggerBinding :one
SELECT b.id, b.original_trigger_id, b.original_assistant_binding_id, b.original_workload_issuer_id,
 b.assistant_binding_generation, b.generation, b.subject, b.deleted,
 COALESCE(NOT b.deleted AND b.project_ref_id IS NOT NULL AND b.trigger_id IS NOT NULL
   AND b.assistant_binding_id = b.original_assistant_binding_id AND b.workload_issuer_id = b.original_workload_issuer_id
   AND b.trigger_id = b.original_trigger_id AND b.project_ref_id = b.project_id
   AND NOT p.deleted AND NOT t.deleted
   AND b.subject = 'assistant-trigger:' || b.original_trigger_id::text
   AND t.definition_slug <> 'wake' AND t.target_kind = 'assistant' AND t.target_ref = ab.original_assistant_id::text
   AND NOT i.deleted AND NOT i.allow_wildcard_admission
   AND i.issuer = @platform_issuer AND i.jwks_uri = @platform_jwks_uri AND i.project_id = b.project_id
   AND EXISTS (SELECT 1 FROM workload_identity_admissions adm
     WHERE adm.organization_id = b.organization_id AND adm.project_id = b.project_id
       AND adm.workload_issuer_id = b.original_workload_issuer_id AND adm.subject = b.subject
       AND adm.match_kind = 'exact' AND NOT adm.deleted)
   AND EXISTS (SELECT 1 FROM workload_agent_assignments wa
     WHERE wa.organization_id = b.organization_id AND wa.workload_issuer_id = b.original_workload_issuer_id
       AND wa.subject = b.subject AND wa.match_kind = 'exact' AND NOT wa.deleted
       AND wa.agent_id = ab.original_agent_id), false)::boolean AS eligible
FROM trigger_workload_bindings b
LEFT JOIN projects p ON p.organization_id = b.organization_id AND p.id = b.project_ref_id
LEFT JOIN trigger_instances t ON t.organization_id = b.organization_id AND t.project_id = b.project_id AND t.id = b.trigger_id
LEFT JOIN assistant_agent_bindings ab ON ab.organization_id = b.organization_id AND ab.project_id = b.project_id AND ab.id = b.assistant_binding_id
LEFT JOIN workload_issuers i ON i.organization_id = b.organization_id AND i.project_id = b.project_id AND i.id = b.workload_issuer_id
WHERE b.organization_id = @organization_id AND b.project_id = @project_id AND b.original_trigger_id = @trigger_id
ORDER BY b.generation DESC LIMIT 1;

-- name: CreateAssistantBinding :one
INSERT INTO assistant_agent_bindings (
 organization_id, project_id, project_ref_organization_id, project_ref_id,
 original_assistant_id, assistant_ref_organization_id, assistant_ref_project_id, assistant_id,
 original_agent_id, agent_ref_organization_id, agent_ref_project_id, agent_id, generation
) VALUES (@organization_id, @project_id, @organization_id, @project_id,
 @assistant_id, @organization_id, @project_id, @assistant_id,
 @agent_id, @organization_id, @project_id, @agent_id, 1)
RETURNING id;

-- name: GetPlatformIssuer :many
SELECT id, deleted, jwks_uri, allow_wildcard_admission FROM workload_issuers
WHERE organization_id = @organization_id AND project_id = @project_id AND issuer = @platform_issuer AND NOT deleted
ORDER BY created_at, id FOR UPDATE;

-- name: CreatePlatformIssuer :one
INSERT INTO workload_issuers (organization_id, project_id, name, issuer, jwks_uri, allow_wildcard_admission)
VALUES (@organization_id, @project_id, @name, @issuer, @jwks_uri, false)
ON CONFLICT (project_id, name) WHERE deleted IS FALSE DO NOTHING
RETURNING id;

-- name: CreateTriggerBinding :exec
INSERT INTO trigger_workload_bindings (
 organization_id, project_id, project_ref_organization_id, project_ref_id,
 original_trigger_id, trigger_ref_organization_id, trigger_ref_project_id, trigger_id,
 original_assistant_binding_id, assistant_binding_ref_organization_id, assistant_binding_ref_project_id, assistant_binding_id,
 assistant_binding_generation, original_workload_issuer_id, workload_issuer_ref_organization_id,
 workload_issuer_ref_project_id, workload_issuer_id, subject, generation
) VALUES (@organization_id, @project_id, @organization_id, @project_id,
 @trigger_id, @organization_id, @project_id, @trigger_id,
 @assistant_binding_id, @organization_id, @project_id, @assistant_binding_id,
 @assistant_generation, @issuer_id, @organization_id, @project_id, @issuer_id, @subject, @generation);

-- name: CreateAdmission :exec
INSERT INTO workload_identity_admissions (organization_id, project_id, workload_issuer_id, subject, match_kind)
VALUES (@organization_id, @project_id, @issuer_id, @subject, 'exact');

-- name: CreateAssignment :exec
INSERT INTO workload_agent_assignments (organization_id, workload_issuer_id, subject, match_kind, agent_id)
VALUES (@organization_id, @issuer_id, @subject, 'exact', @agent_id);

-- name: TombstoneTriggerBinding :exec
UPDATE trigger_workload_bindings SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id AND original_trigger_id = @trigger_id AND NOT deleted;

-- name: TombstoneAssistantBinding :exec
UPDATE assistant_agent_bindings SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id AND original_assistant_id = @assistant_id AND NOT deleted;

-- name: ListAssistantTriggerHistory :many
SELECT DISTINCT tb.original_trigger_id
FROM trigger_workload_bindings tb
JOIN assistant_agent_bindings ab ON ab.id = tb.original_assistant_binding_id
 AND ab.organization_id = tb.organization_id AND ab.project_id = tb.project_id
WHERE ab.organization_id = @organization_id AND ab.project_id = @project_id AND ab.original_assistant_id = @assistant_id
 AND NOT tb.deleted
ORDER BY tb.original_trigger_id;

-- name: RevokeAdmission :exec
UPDATE workload_identity_admissions SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id AND workload_issuer_id = @issuer_id AND subject = @subject AND match_kind = 'exact' AND NOT deleted;

-- name: RevokeAssignment :exec
UPDATE workload_agent_assignments wa SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE wa.organization_id = @organization_id AND wa.workload_issuer_id = @issuer_id AND wa.subject = @subject AND wa.match_kind = 'exact' AND NOT wa.deleted
 AND NOT EXISTS (SELECT 1 FROM workload_identity_admissions adm WHERE adm.organization_id = wa.organization_id AND adm.workload_issuer_id = wa.workload_issuer_id AND adm.subject = wa.subject AND adm.match_kind = wa.match_kind AND NOT adm.deleted);

-- name: RevokeDedicatedAgent :exec
UPDATE agents SET revoked_at = COALESCE(revoked_at, clock_timestamp()), suspended_at = NULL,
 updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND project_id = @project_id AND id = @agent_id AND revoked_at IS NULL;

-- name: LockDedicatedAgent :one
SELECT g.id FROM agents g
JOIN users u ON u.id = g.owner_user_id
JOIN organization_user_relationships m ON m.user_id = u.id AND m.organization_id = g.organization_id
WHERE g.organization_id = @organization_id AND g.project_id = @project_id AND g.id = @agent_id
 AND NOT g.deleted AND g.suspended_at IS NULL AND g.revoked_at IS NULL AND g.owner_reassignment_required_at IS NULL
 AND u.deleted_at IS NULL AND u.workos_deleted_at IS NULL AND NOT m.deleted
FOR UPDATE OF g FOR SHARE OF u, m;

-- name: ListConfiguredMCPServers :many
SELECT DISTINCT s.id
FROM mcp_servers s JOIN projects p ON p.id = s.project_id
WHERE p.organization_id = @organization_id AND p.id = @project_id AND NOT p.deleted AND NOT s.deleted
AND (EXISTS (SELECT 1 FROM assistant_mcp_servers am
  WHERE am.project_id = s.project_id AND am.assistant_id = @assistant_id AND am.mcp_server_id = s.id)
 OR EXISTS (SELECT 1 FROM assistant_toolsets at
  JOIN toolsets ts ON ts.id = at.toolset_id AND ts.project_id = at.project_id AND NOT ts.deleted AND ts.mcp_enabled
  WHERE at.project_id = s.project_id AND at.assistant_id = @assistant_id AND at.toolset_id = s.toolset_id))
ORDER BY s.id;

-- Mirrors LoadAssistantSkills' active/resolvable distribution predicates.
-- name: ListConfiguredSkills :many
SELECT DISTINCT s.id FROM skill_distributions sd
JOIN projects p ON p.id = sd.project_id
JOIN skills s ON s.id = sd.skill_id AND s.project_id = sd.project_id AND s.archived_at IS NULL
WHERE p.organization_id = @organization_id AND p.id = @project_id AND NOT p.deleted
 AND sd.assistant_id = @assistant_id AND sd.channel = 'assistant' AND sd.plugin_id IS NULL AND sd.revoked_at IS NULL
 AND EXISTS (SELECT 1 FROM skill_versions sv WHERE sv.skill_id = sd.skill_id AND sv.spec_valid IS TRUE
   AND (sd.pinned_version_id IS NULL OR sv.id = sd.pinned_version_id))
ORDER BY s.id;

-- Provenance is immutable audit data, distinct from the revocable association.
-- name: RecordProvisioning :exec
INSERT INTO audit_logs (organization_id, project_id, actor_id, actor_type, action, subject_id, subject_type, metadata)
VALUES (@organization_id, @project_id, @actor_user_id, 'user', 'assistant:identity_provision', @assistant_id, 'assistant', @metadata);

-- The following package-local fixtures exercise corrupted/hard-deleted states
-- that public management APIs intentionally cannot produce.
-- name: FixtureCreateOrganization :exec
INSERT INTO organization_metadata (id,name,slug) VALUES (@id,'Identity test',@id);

-- name: FixtureCreateUser :exec
INSERT INTO users (id,email,display_name) VALUES (@id,@email,'Identity user');

-- name: FixtureCreateMembership :exec
INSERT INTO organization_user_relationships (organization_id,user_id) VALUES (@organization_id,@user_id);

-- name: FixtureCreateProject :exec
INSERT INTO projects (id,organization_id,name,slug) VALUES (@id,@organization_id,'Identity project','identity-project');

-- name: FixtureCreateAssistant :exec
INSERT INTO assistants (id,organization_id,project_id,created_by_user_id,name,model,instructions)
VALUES (@id::uuid,@organization_id,@project_id,@creator,'Identity assistant ' || (@id::uuid)::text,'test-model','test instructions');

-- name: FixtureCreateRoot :exec
INSERT INTO trigger_instances (id,organization_id,project_id,definition_slug,name,target_kind,target_ref,target_display)
VALUES (@id,@organization_id,@project_id,@definition_slug,'Identity root','assistant',@target_ref,'Assistant');

-- name: FixtureCreateRemote :exec
INSERT INTO remote_mcp_servers (id,project_id,transport_type,url) VALUES (@id,@project_id,'streamable-http','https://example.com/mcp');

-- name: FixtureCreateMCPServer :exec
INSERT INTO mcp_servers (id,project_id,remote_mcp_server_id,visibility) VALUES (@id,@project_id,@remote_id,'private');

-- name: FixtureAttachMCPServer :exec
INSERT INTO assistant_mcp_servers (assistant_id,mcp_server_id,project_id) VALUES (@assistant_id,@server_id,@project_id);

-- name: FixtureAuthorityCounts :one
SELECT
 (SELECT count(*) FROM agents AS c0 WHERE c0.organization_id = @organization_id)::bigint AS agents,
 (SELECT count(*) FROM assistant_agent_bindings AS c1 WHERE c1.organization_id = @organization_id)::bigint AS assistants,
 (SELECT count(*) FROM workload_issuers AS c2 WHERE c2.organization_id = @organization_id)::bigint AS issuers,
 (SELECT count(*) FROM workload_identity_admissions AS c3 WHERE c3.organization_id = @organization_id)::bigint AS admissions,
 (SELECT count(*) FROM workload_agent_assignments AS c4 WHERE c4.organization_id = @organization_id)::bigint AS assignments,
 (SELECT count(*) FROM trigger_workload_bindings AS c5 WHERE c5.organization_id = @organization_id)::bigint AS triggers,
 (SELECT count(*) FROM principal_grants c6 WHERE c6.organization_id = @organization_id)::bigint AS grants,
 (SELECT count(*) FROM audit_logs c7 WHERE c7.organization_id = @organization_id)::bigint AS audits;

-- name: FixtureProvisioningMetadata :one
SELECT metadata FROM audit_logs WHERE organization_id = @organization_id AND action='assistant:identity_provision' AND subject_id = @subject_id;

-- name: FixtureAgentOwner :one
SELECT owner_user_id FROM agents WHERE organization_id = @organization_id AND id = @agent_id;

-- name: FixtureLiveAssignmentCount :one
SELECT count(*)::bigint FROM workload_agent_assignments WHERE organization_id = @organization_id AND agent_id = @agent_id AND NOT deleted;

-- name: FixtureSetAssistantStatus :exec
UPDATE assistants SET status = @status WHERE organization_id = @organization_id AND project_id = @project_id AND id = @assistant_id;

-- name: FixtureSetTriggerStatus :exec
UPDATE trigger_instances SET status = @status WHERE organization_id = @organization_id AND project_id = @project_id AND id = @trigger_id;

-- name: FixtureRetargetTrigger :exec
UPDATE trigger_instances SET target_ref = @target_ref WHERE organization_id = @organization_id AND project_id = @project_id AND id = @trigger_id;

-- name: FixtureWithdrawIssuer :exec
UPDATE workload_issuers SET deleted_at=clock_timestamp() WHERE organization_id = @organization_id AND id = @issuer_id;

-- name: FixtureSuspendAgent :exec
UPDATE agents SET suspended_at=clock_timestamp() WHERE organization_id = @organization_id AND id = @agent_id;

-- name: FixtureLatchOwner :exec
UPDATE agents SET owner_reassignment_required_at=clock_timestamp(),owner_reassignment_reason='test' WHERE organization_id = @organization_id AND id = @agent_id;

-- name: FixtureDeleteOwner :exec
UPDATE users SET deleted_at=clock_timestamp() WHERE id = @user_id;

-- name: FixtureWithdrawMembership :exec
UPDATE organization_user_relationships SET deleted_at=clock_timestamp() WHERE organization_id = @organization_id AND user_id = @user_id;

-- name: FixtureDeleteAssistant :exec
DELETE FROM assistants WHERE organization_id = @organization_id AND project_id = @project_id AND id = @assistant_id;

-- name: FixtureDeleteAgent :exec
DELETE FROM agents WHERE organization_id = @organization_id AND id = @agent_id;

-- name: FixtureDeleteTrigger :exec
DELETE FROM trigger_instances WHERE organization_id = @organization_id AND project_id = @project_id AND id = @trigger_id;

-- name: FixtureDeleteIssuer :exec
DELETE FROM workload_issuers WHERE organization_id = @organization_id AND id = @issuer_id;

-- name: FixtureDeleteProject :exec
DELETE FROM projects WHERE organization_id = @organization_id AND id = @project_id;

-- name: FixtureSetTriggerDefinition :exec
UPDATE trigger_instances SET definition_slug = @definition_slug
WHERE organization_id = @organization_id AND project_id = @project_id AND id = @trigger_id;

-- Lock all tenant issuers before the agent. This covers non-root assignments
-- and prevents an admission on another existing issuer racing the sweep.
-- name: FixtureWithdrawAssignment :exec
UPDATE workload_agent_assignments SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id AND workload_issuer_id = @issuer_id AND subject = @subject AND match_kind = 'exact' AND NOT deleted;
