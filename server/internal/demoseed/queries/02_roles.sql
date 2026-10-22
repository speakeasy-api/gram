-- Custom roles, plugins, Slack and directory sync, devices, AI scan targets,
-- and the billing contract. Every tenant identifier arrives as a parameter.

-- name: RolesDeleteCustomRoleGrants :exec
DELETE FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn LIKE 'role:organization:%';

-- name: RolesDeleteAgentRoleAssignments :exec
DELETE FROM agent_role_assignments WHERE organization_id = @organization_id;

-- name: RolesDeleteOrganizationRoles :exec
DELETE FROM organization_roles WHERE organization_id = @organization_id;

-- name: RolesInsertCustomRole :one
INSERT INTO organization_roles
  (organization_id, workos_slug, workos_name, workos_description,
   workos_created_at, workos_updated_at)
VALUES (@organization_id, @slug, @name, @description::text, now() - interval '90 days', now())
RETURNING id;

-- Unrestricted selectors: the wildcard shape every scope stores when a rule
-- is not narrowed to one resource. log:read is granted per project, so the
-- Tool Log Viewer names the demo project instead.
-- name: RolesInsertCustomRoleGrants :exec
INSERT INTO principal_grants
  (organization_id, principal_urn, scope, selectors)
SELECT @organization_id::text, @principal_urn::text, scope,
       jsonb_build_object(
         'resource_kind', CASE WHEN scope IN ('plugin:write', 'log:read') THEN 'project'
                               ELSE split_part(scope, ':', 1) END,
         'resource_id', CASE WHEN scope = 'log:read' THEN @project_id::text ELSE '*' END)
FROM unnest(@scopes::text[]) AS scope;

-- name: RolesInsertRoleAssignment :exec
INSERT INTO organization_role_assignments
  (organization_id, workos_user_id, user_id, role_urn, workos_updated_at)
VALUES (@organization_id, @workos_user_id, @user_id::text, @role_urn, now());

-- name: RolesInsertAgentRoleAssignment :exec
INSERT INTO agent_role_assignments (organization_id, agent_id, role_urn)
VALUES (@organization_id, @agent_id, @role_urn);

-- name: RolesInsertDefaultPlugin :one
INSERT INTO plugins (organization_id, project_id, name, slug, is_default)
VALUES (@organization_id, @project_id, 'Default', 'default', true)
RETURNING id;

-- name: RolesInsertEveryonePluginAssignment :exec
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
VALUES (@plugin_id, @organization_id, '*');

-- Every role (the tenant's custom ones, then the global ones) with the plugin
-- slug it maps to. The slug matches conv.ToSlug, including Go's ASCII-only
-- whitespace class.
-- name: RolesListRolesForPlugins :many
SELECT r.role_urn::text AS role_urn, r.name::text AS name,
       trim(BOTH '-' FROM regexp_replace(
         lower(regexp_replace(r.name, E'[^a-zA-Z0-9 \t\n\f\r-]', '', 'g')),
         E'[- \t\n\f\r]+', '-', 'g'))::text AS slug
FROM (
  SELECT 'role:organization:' || id AS role_urn, workos_name AS name
  FROM organization_roles WHERE organization_id = @organization_id AND deleted IS FALSE AND workos_deleted IS FALSE
  UNION ALL
  SELECT 'role:global:' || id, workos_name
  FROM global_roles WHERE deleted IS FALSE AND workos_deleted IS FALSE
) r;

-- name: RolesFindPluginBySlug :one
SELECT id FROM plugins
WHERE organization_id = @organization_id AND project_id = @project_id AND deleted IS FALSE
  AND slug = @slug
ORDER BY created_at, id LIMIT 1;

-- name: RolesInsertRolePlugin :one
INSERT INTO plugins (organization_id, project_id, name, slug, auto_created)
VALUES (@organization_id, @project_id, @name, @slug, true)
RETURNING id;

-- name: RolesInsertRolePluginAssignment :exec
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
VALUES (@plugin_id, @organization_id, @principal_urn)
ON CONFLICT (plugin_id, principal_urn) DO NOTHING;

-- The Read-only Tools role is the disposition case: it reaches every server,
-- but only tools annotated read-only. That narrowing is a dimension on the
-- selector, not a separate scope.
-- name: RolesSetReadOnlyDisposition :exec
UPDATE principal_grants
SET selectors = selectors || jsonb_build_object('disposition', 'read_only')
WHERE principal_grants.organization_id = @organization_id::text
  AND scope = 'mcp:connect'
  AND principal_urn = (
    SELECT 'role:organization:' || r.id FROM organization_roles r
    WHERE r.organization_id = @organization_id::text AND r.workos_slug = 'read-only-tools');

-- name: RolesDeleteSlackMemberships :exec
DELETE FROM slack_directory_memberships WHERE organization_id = @organization_id;

-- name: RolesDeleteSlackConnections :exec
DELETE FROM slack_directory_connections WHERE organization_id = @organization_id;

-- Synthetic workspace history has no usable credentials or claimed
-- authorization.
-- name: RolesInsertSlackConnections :exec
INSERT INTO slack_directory_connections
  (id, organization_id, slack_team_id, slack_team_name, generation, health, disconnected_at)
VALUES
  (@conn1_id, @organization_id, 'T0DEMO0001', 'Acme Engineering',
   gen_random_uuid(), 'pending', NULL),
  (@conn2_id, @organization_id, 'T0DEMO0002', 'Acme Operations',
   gen_random_uuid(), 'pending', NULL);

-- Synthetic retained snapshots illustrate history, not a live Slack sync.
-- name: RolesMarkSlackConnectionsSynced :exec
UPDATE slack_directory_connections
SET last_full_sync_generation = gen_random_uuid(),
    last_full_sync_succeeded_at = now() - interval '3 days',
    last_sync_started_at = now() - interval '3 days 1 minute'
WHERE organization_id = @organization_id;

-- name: RolesInsertSlackMembership :exec
INSERT INTO slack_directory_memberships
  (id, organization_id, slack_team_id, slack_user_id, display_name, email,
   status, member_type, last_seen_at)
VALUES (@id, @organization_id, @slack_team_id, @slack_user_id, @display_name::text,
        sqlc.narg(email)::text, @status, @member_type, now() - (@last_seen_ago::text)::interval);

-- Synthetic admin decisions remain visible in disconnected directory history.
-- name: RolesInsertSlackIdentityMapping :exec
INSERT INTO slack_identity_mappings (id, organization_id, slack_team_id, slack_user_id, user_id)
SELECT @id::uuid, @organization_id::text, m.slack_team_id, m.slack_user_id, @user_id::text
FROM slack_directory_memberships m
WHERE m.organization_id = @organization_id::text AND m.id = @membership_id::uuid;

-- name: RolesMarkSlackMappingConflicts :exec
UPDATE slack_directory_memberships SET mapping_revision = 1,
  mapping_conflict_reason = CASE slack_user_id
    WHEN 'U0DEMO00003' THEN 'member_deactivated'
    WHEN 'U0DEMO00005' THEN 'member_absent'
    WHEN 'U0DEMO00007' THEN 'member_deactivated'
    WHEN 'U0DEMO00010' THEN 'member_deactivated' END,
  mapping_conflict_detected_at = CASE WHEN slack_user_id IN ('U0DEMO00003', 'U0DEMO00005', 'U0DEMO00007', 'U0DEMO00010') THEN now() - interval '2 days' END
WHERE organization_id = @organization_id AND slack_user_id IN ('U0DEMO00001', 'U0DEMO00003', 'U0DEMO00005', 'U0DEMO00006', 'U0DEMO00007', 'U0DEMO00010');

-- name: RolesDeleteDirectoryRoleMappings :exec
DELETE FROM directory_role_mappings WHERE organization_id = @organization_id;

-- name: RolesDeleteDirectoryReviewItems :exec
DELETE FROM directory_review_items WHERE organization_id = @organization_id;

-- name: RolesDeleteDirectoryReviewOrgState :exec
DELETE FROM directory_review_org_states WHERE organization_id = @organization_id;

-- name: RolesDeleteDirectoryReviewOptOuts :exec
DELETE FROM directory_review_attribute_opt_outs WHERE organization_id = @organization_id;

-- Starts directory review now: groups created before this are offered in
-- bulk setup, groups created after it are raised for review. Review emails
-- are off: the seeded items are recreated on every reseed, so they would
-- otherwise email the organization's admins each day.
-- name: RolesInsertDirectoryReviewOrgState :exec
INSERT INTO directory_review_org_states (organization_id, baseline_at, emails_disabled_at)
VALUES (@organization_id::text, clock_timestamp(), clock_timestamp());

-- A group first seen after review started.
-- name: RolesInsertNewDirectoryGroup :exec
INSERT INTO directory_groups
  (organization_id, workos_directory_group_id, name, workos_created_at, workos_updated_at)
VALUES (@organization_id::text, @group_prefix::text || @slug::text, @name::text, now(), now());

-- name: RolesInsertDirectoryReviewGroupItem :exec
INSERT INTO directory_review_items (organization_id, kind, directory_group_id, occurrence, state)
SELECT @organization_id::text, @kind::text, dg.id, 1, 'pending'
FROM directory_groups dg
WHERE dg.organization_id = @organization_id::text AND dg.name = @group_name::text;

-- name: RolesInsertDirectoryReviewAttributeItem :exec
INSERT INTO directory_review_items (organization_id, kind, attribute_key, attribute_value, occurrence, state)
VALUES (@organization_id::text, @kind::text, @attribute_key::text, @attribute_value::text, 1, 'pending');

-- name: RolesDeleteDirectoryGroupMemberships :exec
DELETE FROM directory_user_group_memberships WHERE directory_group_id IN
  (SELECT id FROM directory_groups WHERE organization_id = @organization_id);

-- name: RolesDeleteDirectoryUsers :exec
DELETE FROM directory_users WHERE organization_id = @organization_id;

-- name: RolesDeleteDirectoryGroups :exec
DELETE FROM directory_groups WHERE organization_id = @organization_id;

-- One group per distinct team, its id the tenant's group prefix plus the
-- lowercased, dashed team name.
-- name: RolesInsertDirectoryGroups :exec
INSERT INTO directory_groups
  (organization_id, workos_directory_group_id, name, workos_created_at, workos_updated_at)
SELECT @organization_id::text, @group_prefix::text || replace(lower(t), ' ', '-'), t, now(), now()
FROM (SELECT DISTINCT unnest(@teams::text[]) AS t) g;

-- A group the directory has since deleted. Its mapping stays and grants
-- nothing, so the mappings page shows it as a missing source.
-- name: RolesInsertDeletedDirectoryGroup :exec
INSERT INTO directory_groups
  (organization_id, workos_directory_group_id, name, workos_created_at,
   workos_updated_at, workos_deleted_at, deleted_at)
VALUES (@organization_id::text, @group_prefix::text || @slug::text, @name::text,
        now() - interval '60 days', now() - interval '3 days',
        now() - interval '3 days', now() - interval '3 days');

-- name: RolesInsertDirectoryUser :exec
-- user_id is NULL for directory people who have not signed in yet.
INSERT INTO directory_users
  (organization_id, user_id, workos_directory_user_id, email, first_name,
   last_name, attributes, workos_created_at, workos_updated_at)
VALUES (@organization_id, sqlc.narg(user_id)::text, @workos_directory_user_id,
        @email::text, sqlc.narg(first_name)::text, sqlc.narg(last_name)::text,
        jsonb_build_object(
          'division_name', @division_name::text,
          'department_name', @department_name::text,
          'job_title', @job_title::text,
          'employee_type', @employee_type::text,
          'cost_center_name', @cost_center_name::text),
        now(), now());

-- name: RolesInsertDirectoryGroupMembership :exec
INSERT INTO directory_user_group_memberships
  (directory_user_id, directory_group_id, workos_directory_user_id,
   workos_directory_group_id, workos_created_at)
SELECT du.id, dg.id, du.workos_directory_user_id, dg.workos_directory_group_id, now()
FROM directory_users du, directory_groups dg
WHERE du.workos_directory_user_id = @workos_directory_user_id::text
  AND dg.organization_id = @organization_id::text AND dg.name = @team::text;

-- name: RolesInsertGroupRoleMapping :exec
INSERT INTO directory_role_mappings
  (organization_id, source_kind, directory_group_id, role_urn)
SELECT @organization_id::text, 'group', dg.id, 'role:organization:' || r.id
FROM directory_groups dg, organization_roles r
WHERE dg.organization_id = @organization_id::text AND dg.name = @group_name::text
  AND r.organization_id = @organization_id::text AND r.workos_slug = @role_slug::text;

-- name: RolesInsertAttributeRoleMapping :exec
INSERT INTO directory_role_mappings
  (organization_id, source_kind, attribute_key, attribute_value, role_urn)
SELECT @organization_id::text, 'attribute', @attribute_key::text, @attribute_value::text,
       'role:organization:' || r.id
FROM organization_roles r
WHERE r.organization_id = @organization_id::text AND r.workos_slug = @role_slug::text;

-- name: RolesDeleteUserAccounts :exec
DELETE FROM user_accounts WHERE organization_id = @organization_id;

-- name: RolesDeleteDeviceOwners :exec
DELETE FROM device_owners WHERE organization_id = @organization_id;

-- name: RolesDeleteDeviceAgentSyncs :exec
DELETE FROM device_agent_syncs WHERE organization_id = @organization_id;

-- name: RolesDeleteDeviceAgentDeviceSyncs :exec
DELETE FROM device_agent_device_syncs WHERE organization_id = @organization_id;

-- name: RolesInsertUserAccount :exec
INSERT INTO user_accounts
  (organization_id, user_id, provider, external_org_id, external_account_uuid,
   external_account_id, email, account_type, billing_mode)
VALUES (@organization_id, @user_id::text, @provider, @external_org_id::text,
        @external_account_uuid, @external_account_id::text, @email::text,
        @account_type::text, @billing_mode::text);

-- name: RolesInsertDeviceOwner :exec
INSERT INTO device_owners (organization_id, provider, device_id, linked_user_id)
VALUES (@organization_id, 'anthropic', @device_id, @linked_user_id::text);

-- name: RolesInsertDeviceAgentSync :exec
INSERT INTO device_agent_syncs (organization_id, email, first_seen_at, last_seen_at)
VALUES (@organization_id, @email, now() - interval '9 days',
        now() - (@last_seen_ago::text)::interval);

-- name: RolesDeleteAIScanTargets :exec
DELETE FROM ai_scan_targets WHERE organization_id = @organization_id;

-- The decided built-ins carry no definition: the compiled-in one stays
-- authoritative, so a later registry revision still reaches this org.
-- name: RolesInsertAIScanTargets :exec
INSERT INTO ai_scan_targets (organization_id, id, status, rationale)
VALUES
  (@organization_id, 'claude-code', 'approved',
   'Standard issue for the platform team.'),
  (@organization_id, 'codex', 'blocked',
   'Not covered by the vendor review. Ask in #ai-tooling if you need it.'),
  (@organization_id, 'hermes-agent', 'approved',
   'Reviewed with the research team. Publishes a client ID metadata '
   || 'document, so the approval is enforced at the gateway.'),
  (@organization_id, 'zed', 'approved',
   'Approved for the platform team after the editor review.'),
  (@organization_id, 'goose', 'blocked',
   'Runs arbitrary local commands under its own extensions. Blocked until '
   || 'the extension policy lands.');

-- name: RolesDeleteMDMDevices :exec
DELETE FROM mdm_devices WHERE organization_id = @organization_id;

-- name: RolesDeleteDeviceIntegrationConfigs :exec
DELETE FROM device_integration_configs WHERE organization_id = @organization_id;

-- name: RolesInsertJamfIntegration :exec
INSERT INTO device_integration_configs
  (id, organization_id, provider, credentials_encrypted, settings, enabled)
VALUES (@id, @organization_id, 'jamf',
        'DEMO-ENCRYPTED-CREDENTIAL-NOT-A-SECRET',
        @settings::jsonb, TRUE);

-- name: RolesInsertMDMDevice :exec
INSERT INTO mdm_devices
  (device_integration_config_id, organization_id, external_id, serial_number,
   hostname, os_name, os_version, user_email, user_id, mdm_last_check_in_at,
   first_seen_at, last_seen_at, missing_since)
VALUES (@device_integration_config_id, @organization_id, @external_id,
        @serial_number::text, @hostname::text, @os_name::text, @os_version::text,
        @user_email::text, sqlc.narg(user_id)::text,
        now() - (@check_in_ago::text)::interval,
        now() - interval '11 days', now() - (@check_in_ago::text)::interval, NULL);

-- name: RolesInsertDeviceAgentDeviceSync :exec
INSERT INTO device_agent_device_syncs
  (organization_id, serial_number, email, hostname, first_seen_at, last_seen_at)
VALUES (@organization_id, @serial_number, @email, @hostname::text,
        now() - interval '11 days', now() - (@last_seen_ago::text)::interval);

-- Enterprise billing contract: without a contracted TUM baseline the Billing
-- page's Platform+Overage estimate shows "Requires a contracted baseline",
-- which reads broken for an enterprise org.
-- name: RolesUpsertBillingContract :exec
INSERT INTO billing_metadata (organization_id, tum_monthly_token_limit, billing_cycle_anchor_day)
VALUES (@organization_id, 50000000, 1)
ON CONFLICT (organization_id) DO UPDATE
  SET tum_monthly_token_limit = EXCLUDED.tum_monthly_token_limit;
