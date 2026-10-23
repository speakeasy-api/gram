-- Preflight isolation asserts, org metadata and onboarding, the scoped purge,
-- the project, features, users, and memberships.

-- name: OrgDeleteSigintDefinitionSnapshots :exec
DELETE FROM sigint_definition_snapshots
WHERE project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgInsertSigintDefinitionSnapshot :exec
INSERT INTO sigint_definition_snapshots (project_id, sensor_id, definition_hash, definition)
VALUES (@project_id::uuid, @sensor_id::uuid, @definition_hash, @definition::jsonb);

-- name: OrgCountSigintDefinitionSnapshots :one
SELECT count(*) FROM sigint_definition_snapshots WHERE project_id = @project_id::uuid;

-- name: OrgForeignOrgExists :one
-- workos_id is compared against this tenant's expected link rather than merely
-- tested for NULL: the shared demo org never links to WorkOS (its constant is
-- an inert placeholder that matches nothing), but the local development tenant
-- IS a real dev-idp org, and its auth callback stamps that link here on first
-- login. Any OTHER workos_id means this id belongs to somebody else.
SELECT EXISTS (
  SELECT 1 FROM organization_metadata
  WHERE id = @org_id::text
    AND ((workos_id IS NOT NULL AND workos_id <> @workos_id::text) OR slug <> @org_slug::text)
) AS found;

-- name: OrgForeignProjectExists :one
SELECT EXISTS (
  SELECT 1 FROM projects
  WHERE id = @project_id::uuid AND organization_id <> @org_id::text
) AS found;

-- name: OrgUpsertMetadata :exec
-- 'enterprise' account type: the demo must showcase enterprise-gated surfaces
-- (Logs page and other EnterpriseGate features). Demo identity is carried by
-- the fixed org id (constants.DemoOrganizationID) — NOT by account type, which
-- the auth callback overwrites anyway.
INSERT INTO organization_metadata (id, name, slug, gram_account_type, whitelisted, scim_enabled)
VALUES (@org_id::text, @org_name::text, @org_slug::text, 'enterprise', TRUE, TRUE)
ON CONFLICT (id) DO UPDATE
  -- whitelisted is repaired, not just set on insert: a developer who logged
  -- in before seeding already has this row, created un-whitelisted by the
  -- auth callback, and without this the seed leaves them on the BookDemo
  -- gate.
  SET name = EXCLUDED.name, slug = EXCLUDED.slug,
      gram_account_type = EXCLUDED.gram_account_type,
      whitelisted = EXCLUDED.whitelisted,
      scim_enabled = EXCLUDED.scim_enabled;

-- name: OrgUpsertOnboarding :exec
INSERT INTO organization_onboarding (organization_id, preset, mdm_vendor, mdm_vendor_name)
VALUES (@org_id::text, 'security', 'jamf', NULL)
ON CONFLICT (organization_id) DO UPDATE
  SET preset = EXCLUDED.preset,
      mdm_vendor = EXCLUDED.mdm_vendor,
      mdm_vendor_name = EXCLUDED.mdm_vendor_name,
      updated_at = clock_timestamp();

-- name: OrgDeleteOnboardingVendors :exec
DELETE FROM organization_onboarding_vendors WHERE organization_id = @org_id::text;

-- name: OrgInsertOnboardingVendors :exec
-- The stack staff recorded: Anthropic on Enterprise and Cursor on Teams. Plans
-- resolve by slug from the support matrix catalog the admin server seeds; a
-- plan the catalog does not hold yet leaves the vendor planless.
INSERT INTO organization_onboarding_vendors (organization_id, vendor, plan_id)
VALUES
  (@org_id::text, 'Anthropic', (SELECT id FROM support_matrix_plans WHERE slug = 'anthropic-enterprise' AND deleted_at IS NULL)),
  (@org_id::text, 'Cursor', (SELECT id FROM support_matrix_plans WHERE slug = 'cursor-teams' AND deleted_at IS NULL));

-- name: OrgDeleteKillswitchOperations :exec
DELETE FROM killswitch_operations WHERE organization_id = @org_id::text;

-- name: OrgDeleteKillswitchPrescriptions :exec
DELETE FROM killswitch_prescriptions WHERE organization_id = @org_id::text;

-- name: OrgDeleteNetworkIngresses :exec
DELETE FROM network_ingresses WHERE organization_id = @org_id::text;

-- name: OrgDeleteNetworkIngressFeature :exec
DELETE FROM organization_features
WHERE organization_id = @org_id::text AND feature_name = 'network_ingress';

-- name: OrgDeletePlatformMCPSelectedUseEvidence :exec
-- project_id is NOT NULL and pinned to an org project by the composite FK,
-- so this covers evidence for every workflow deleted below.
DELETE FROM platform_mcp_selected_use_evidence
  WHERE organization_id = @org_id::text
    AND project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeletePlatformMCPFeedback :exec
DELETE FROM platform_mcp_feedback
  WHERE organization_id = @org_id::text
    AND (project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text) OR workflow_id IN
      (SELECT id FROM platform_mcp_onboarding_workflows
       WHERE organization_id = @org_id::text AND selected_project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text)));

-- name: OrgDeletePlatformMCPOnboardingMilestones :exec
DELETE FROM platform_mcp_onboarding_milestones
  WHERE organization_id = @org_id::text AND project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeletePlatformMCPOnboardingWorkflows :exec
DELETE FROM platform_mcp_onboarding_workflows
  WHERE organization_id = @org_id::text AND selected_project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeletePlatformMCPOperationReceipts :exec
DELETE FROM platform_mcp_operation_receipts
  WHERE organization_id = @org_id::text AND project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeletePlatformMCPSetupHandoffs :exec
DELETE FROM platform_mcp_setup_handoffs
  WHERE organization_id = @org_id::text AND project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeletePlatformMCPReadiness :exec
DELETE FROM platform_mcp_readiness
  WHERE organization_id = @org_id::text AND project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeletePlatformMCPDistributions :exec
DELETE FROM platform_mcp_distributions
  WHERE organization_id = @org_id::text AND project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeletePlatformMCPCatalogRegistrations :exec
DELETE FROM platform_mcp_catalog_registrations
  WHERE organization_id = @org_id::text AND project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeleteTriggerWorkloadBindings :exec
DELETE FROM trigger_workload_bindings
  WHERE organization_id = @org_id::text AND project_id = @project_id::uuid;

-- name: OrgDeleteAssistantAgentBindings :exec
DELETE FROM assistant_agent_bindings
  WHERE organization_id = @org_id::text AND project_id = @project_id::uuid;

-- name: OrgDeleteAssistantMCPServers :exec
DELETE FROM assistant_mcp_servers WHERE project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeletePluginServers :exec
DELETE FROM plugin_servers WHERE plugin_id IN
  (SELECT id FROM plugins
   WHERE organization_id = @org_id::text);

-- name: OrgDeletePrincipalRemoteSessionBindings :exec
DELETE FROM principal_remote_session_bindings WHERE organization_id = @org_id::text;

-- name: OrgDeleteRemoteSessions :exec
DELETE FROM remote_sessions WHERE user_session_issuer_id IN
  (SELECT id FROM user_session_issuers WHERE project_id = @project_id::uuid OR organization_id = @org_id::text)
  OR remote_session_client_id IN
  (SELECT id FROM remote_session_clients WHERE project_id = @project_id::uuid OR organization_id = @org_id::text);

-- Cached EMA credentials and the trusted issuer sessions they cite SET NULL
-- on their parents, so clear them by tenant first.
-- name: OrgDeleteRemoteSessionEMACredentials :exec
DELETE FROM remote_session_ema_credentials WHERE organization_id = @org_id::text;

-- name: OrgDeleteTrustedIssuerSessions :exec
DELETE FROM trusted_issuer_sessions WHERE organization_id = @org_id::text;

-- name: OrgDeleteRemoteSessionEMABindings :exec
DELETE FROM remote_session_ema_bindings
WHERE organization_id = @org_id::text
  AND project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeleteRemoteSessionEMAProviders :exec
DELETE FROM remote_session_ema_providers WHERE organization_id = @org_id::text;

-- name: OrgDeleteRemoteSessionClients :exec
DELETE FROM remote_session_clients WHERE project_id = @project_id::uuid OR organization_id = @org_id::text;

-- name: OrgDeleteRemoteSessionIssuers :exec
DELETE FROM remote_session_issuers WHERE project_id = @project_id::uuid OR organization_id = @org_id::text;

-- name: OrgDeleteMCPServers :exec
DELETE FROM mcp_servers WHERE project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeleteRemoteProtectedResources :exec
DELETE FROM remote_protected_resources WHERE project_id = @project_id::uuid;

-- name: OrgDeleteMetaMCPServers :exec
DELETE FROM meta_mcp_servers WHERE organization_id = @org_id::text;

-- name: OrgDeleteUserSessionIssuers :exec
DELETE FROM user_session_issuers WHERE project_id = @project_id::uuid OR organization_id = @org_id::text;

-- name: OrgDeleteExternalOAuthServerMetadata :exec
-- The metadata table is project-scoped rather than organization-scoped, so
-- constrain cleanup through the demo project's tenant ownership.
DELETE FROM external_oauth_server_metadata
WHERE project_id IN (
  SELECT id FROM projects
  WHERE id = @project_id::uuid AND organization_id = @org_id::text
);

-- name: OrgDeleteToolsets :exec
DELETE FROM toolsets WHERE organization_id = @org_id::text;

-- name: OrgDeleteEnvironments :exec
DELETE FROM environments WHERE organization_id = @org_id::text;

-- name: OrgDeleteDeploymentStatuses :exec
DELETE FROM deployment_statuses WHERE deployment_id IN
  (SELECT id FROM deployments WHERE organization_id = @org_id::text);

-- name: OrgDeleteDeploymentLogs :exec
DELETE FROM deployment_logs WHERE deployment_id IN
  (SELECT id FROM deployments WHERE organization_id = @org_id::text);

-- name: OrgDeleteDeployments :exec
DELETE FROM deployments WHERE organization_id = @org_id::text;

-- name: OrgDeleteAssets :exec
DELETE FROM assets WHERE project_id = @project_id::uuid;

-- name: OrgDeleteLitellmInstances :exec
DELETE FROM litellm_instances WHERE organization_id = @org_id::text;

-- name: OrgDeleteAPIKeys :exec
DELETE FROM api_keys WHERE organization_id = @org_id::text;

-- name: OrgDeleteSetupTasks :exec
DELETE FROM organization_setup_tasks WHERE organization_id = @org_id::text;

-- name: OrgDeleteBusinessMemories :exec
DELETE FROM business_memories WHERE organization_id = @org_id::text;

-- name: OrgDeletePolicyBypassGrants :exec
DELETE FROM principal_grants
WHERE organization_id = @org_id::text
  AND scope = 'risk_policy:bypass'
  AND selectors ->> 'resource_id' = @policy_id::text;

-- name: OrgDeleteGrantsByID :exec
DELETE FROM principal_grants
WHERE organization_id = @org_id::text
  AND id = ANY(@ids::uuid[]);

-- name: OrgDeleteWorkloadAgentAssignments :exec
DELETE FROM workload_agent_assignments WHERE organization_id = @org_id::text;

-- name: OrgDeleteWorkloadIdentityAdmissions :exec
DELETE FROM workload_identity_admissions WHERE organization_id = @org_id::text;

-- name: OrgDeleteWorkloadIssuers :exec
DELETE FROM workload_issuers WHERE organization_id = @org_id::text;

-- name: OrgDeleteChatMessageParticipants :exec
DELETE FROM chat_message_participants WHERE project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeleteSigintSensorSignals :exec
DELETE FROM sigint_sensor_signals
WHERE project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeleteSigintSensors :exec
DELETE FROM sigint_sensors
WHERE project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeleteSigintCustomSignals :exec
DELETE FROM sigint_custom_signals
WHERE project_id IN (SELECT id FROM projects WHERE organization_id = @org_id::text);

-- name: OrgDeleteProjects :exec
DELETE FROM projects WHERE organization_id = @org_id::text;

-- name: OrgInsertProject :exec
-- Single project: the demo org intentionally has exactly one project so new
-- users land somewhere obvious.
INSERT INTO projects (id, name, slug, organization_id) VALUES
  (@project_id::uuid, 'Default', 'default', @org_id::text);

-- name: OrgInsertSigintCustomSignal :exec
INSERT INTO sigint_custom_signals
  (id, project_id, name, slug, description, classifier_criteria)
VALUES
  (@id::uuid, @project_id::uuid, @name::text, @slug::text, @description::text,
   sqlc.narg(classifier_criteria)::text);

-- name: OrgInsertSigintSensor :exec
INSERT INTO sigint_sensors
  (id, project_id, name, slug, description, instructions, mode, enabled, input_strategy, window_size)
VALUES
  (@id::uuid, @project_id::uuid, @name::text, @slug::text, @description::text,
   sqlc.narg(instructions)::text, @mode::text, @enabled::boolean,
   @input_strategy::text, @window_size::integer);

-- name: OrgInsertSigintSensorSignal :exec
INSERT INTO sigint_sensor_signals
  (id, project_id, sensor_id, signal_id, sort_order)
VALUES
  (@id::uuid, @project_id::uuid, @sensor_id::uuid, @signal_id::uuid, @sort_order::integer);

-- name: OrgInsertFeatures :exec
-- 'rbac' is required for the agent-sessions list: without it
-- authz.ShouldEnforce is false and chatVisibilityScope falls back to
-- own-sessions-only, hiding every seeded chat (owned by the seeded users).
INSERT INTO organization_features (organization_id, feature_name)
SELECT @org_id::text, f
FROM unnest(@features::text[]) AS f
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;

-- name: OrgInsertGatewayDisabledFeature :exec
-- Start with access review required, preserving an explicit opt-in on reseed.
INSERT INTO organization_features (organization_id, feature_name)
SELECT @org_id::text, 'organization_gateway_disabled'
WHERE NOT EXISTS (
  SELECT 1 FROM organization_features
  WHERE organization_id = @org_id::text AND feature_name = 'organization_gateway_disabled'
)
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;

-- name: OrgInsertHooksFailOpenFeature :exec
-- Unlike demo entitlements, preserve an explicit fail-closed choice on reseed.
INSERT INTO organization_features (organization_id, feature_name)
SELECT @org_id::text, 'hooks_fail_open'
WHERE NOT EXISTS (
  SELECT 1 FROM organization_features
  WHERE organization_id = @org_id::text AND feature_name = 'hooks_fail_open'
)
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING;

-- name: OrgUpsertUser :exec
INSERT INTO users (id, email, display_name, workos_id)
VALUES (@id::text, @email::text, @display_name::text, @workos_id::text)
ON CONFLICT (id) DO UPDATE
  SET email = EXCLUDED.email, display_name = EXCLUDED.display_name,
      workos_id = EXCLUDED.workos_id;

-- name: OrgInsertSetupTasks :exec
-- Customized Security selection: defer Anthropic admin controls and include
-- server distribution. Both views read this same explicit selection.
INSERT INTO organization_setup_tasks
  (organization_id, task_key, status, assignee_user_id, assignee_email, hidden_at)
VALUES
  (@org_id::text, 'identity-provider', 'todo', NULL, NULL, NULL),
  (@org_id::text, 'create-marketplace', 'todo', NULL, NULL, NULL),
  (@org_id::text, 'distribute-servers', 'todo', NULL, NULL, NULL),
  (@org_id::text, 'enable-logging', 'todo', NULL, NULL, NULL),
  (@org_id::text, 'anthropic-observability', 'todo', NULL, NULL, NULL),
  (@org_id::text, 'confirm-traffic', 'todo', NULL, NULL, NULL),
  (@org_id::text, 'anthropic-admin-controls', 'todo', NULL, NULL, now()),
  (@org_id::text, 'instrument-agents', 'in_progress', @instrument_assignee::text, NULL, NULL),
  (@org_id::text, 'additional-agent-config', 'awaiting_support', NULL,
   @support_assignee_email::text, NULL),
  (@org_id::text, 'configure-policies', 'done', NULL, NULL, NULL),
  (@org_id::text, 'platform-mcp', 'todo', NULL, NULL, now());

-- name: OrgDeleteAgentGrants :exec
-- Grants have no agent FK: clear stale policies before deterministic agent
-- IDs are recreated. Keep human grants and every other organization intact.
DELETE FROM principal_grants
WHERE organization_id = @org_id::text
  AND principal_urn LIKE 'agent:%';

-- name: OrgDeleteAgents :exec
-- Agents RESTRICT owner membership deletion and are not project children.
DELETE FROM agents WHERE organization_id = @org_id::text;

-- name: OrgDeleteSlackIdentityMappings :exec
DELETE FROM slack_identity_mappings WHERE organization_id = @org_id::text;

-- name: OrgDeleteMemberships :exec
DELETE FROM organization_user_relationships WHERE organization_id = @org_id::text;

-- name: OrgInsertMembership :exec
-- created_at steps back 40 days per roster position (1-based), as the old
-- loop's now() - interval '40 days' * i did.
INSERT INTO organization_user_relationships
  (organization_id, user_id, workos_user_id, workos_membership_id, created_at)
VALUES (@org_id::text, @user_id::text, @workos_user_id::text,
        @workos_membership_id::text, now() - (interval '40 days' * @position::integer));

-- name: OrgInsertManagedAgents :exec
-- Managed identities are distinct from OAuth client registrations. Existing
-- fictional owners exercise name/initials rendering without adding external
-- avatar dependencies. The two active release agents receive only
-- project/server-scoped direct grants alongside their inert attachments.
INSERT INTO agents
  (id, organization_id, owner_user_id, name, suspended_at, revoked_at)
VALUES
  (@agent1::uuid, @org_id::text, @owner1::text,
   'Release assistant', NULL, NULL),
  (@agent2::uuid, @org_id::text, @owner2::text,
   'Support triage', now() - interval '2 days', NULL),
  (@agent3::uuid, @org_id::text, @owner3::text,
   'Retired documentation bot', NULL, now() - interval '5 days'),
  (@agent4::uuid, @org_id::text, @owner1::text,
   'Release notes assistant', NULL, NULL);

-- name: OrgGlobalRoleURN :one
-- Global roles are synced from WorkOS in real envs; tolerate their absence
-- locally (no row).
SELECT ('role:global:' || id)::text AS role_urn
FROM global_roles WHERE workos_slug = @workos_slug::text AND deleted_at IS NULL LIMIT 1;

-- name: OrgDeleteRoleAssignments :exec
DELETE FROM organization_role_assignments WHERE organization_id = @org_id::text;

-- name: OrgInsertRoleAssignment :exec
INSERT INTO organization_role_assignments
  (organization_id, workos_user_id, user_id, role_urn, workos_updated_at)
VALUES (@org_id::text, @workos_user_id::text, @user_id::text, @role_urn::text, now());
