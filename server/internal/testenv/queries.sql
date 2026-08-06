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

-- name: SetProjectSlugFixture :exec
UPDATE projects
SET slug = @slug
WHERE id = @id;

-- name: UpdateRiskResultCreatedAt :exec
UPDATE risk_results
SET created_at = @created_at
WHERE id = @id;

-- name: UpdateRiskPolicyBypassRequestTimestamps :exec
UPDATE risk_policy_bypass_requests
SET created_at = @requested_at, updated_at = @requested_at
WHERE id = @id
  AND project_id = @project_id;

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
-- Counts enqueued webhook events of a given type. The event type lives in a
-- Pub/Sub message attribute rather than a column now, because the outbox row
-- itself is transport-agnostic.
SELECT COUNT(*)
FROM publish_outbox
WHERE attributes->>'event_type' = @event_type::text;

-- name: ListRiskResultsAll :many
-- Fixture query used by the risk-analysis activity tests that need to
-- inspect dead-letter and "no findings" rows the production queries filter
-- out via `found IS TRUE`.
SELECT *
FROM risk_results
WHERE project_id = @project_id
  AND risk_policy_id = @risk_policy_id
ORDER BY id;

-- name: SeedOutboxEntry :one
-- Fixture insert for the deprecated outbox table. Producers write to
-- publish_outbox now, so the only thing that still needs to create one of
-- these rows is the legacy relay's own tests; this goes away with them.
INSERT INTO outbox (organization_id, event_type, payload)
VALUES (@organization_id, @event_type, @payload)
RETURNING id;

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

-- name: GetPublishOutboxRow :one
SELECT id, public_id, organization_id, topic, message, attributes,
       attempts, last_error, retry_after, locked_until, lease_token, created_at
FROM publish_outbox
WHERE id = @id;

-- name: GetPublishOutboxDeadLetter :one
SELECT id, public_id, organization_id, topic, message, attributes,
       attempts, last_error, enqueued_at, created_at
FROM publish_outbox_dead_letters
WHERE public_id = @public_id;

-- name: CountPublishOutboxRows :one
SELECT COUNT(*) FROM publish_outbox;

-- name: SeedPublishOutboxRow :one
-- Fixture insert that can set the retry/lease columns a producer never touches.
INSERT INTO publish_outbox (
    public_id, organization_id, topic, message, attributes,
    attempts, retry_after, locked_until
)
VALUES (
    COALESCE(sqlc.narg(public_id)::uuid, generate_uuidv7()),
    @organization_id, @topic, @message, @attributes,
    @attempts, sqlc.narg(retry_after), sqlc.narg(locked_until)
)
RETURNING id, public_id;

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

-- name: InsertPluginAssignmentFixture :exec
-- Test-only fixture: writes a plugin_assignments row with an EXPLICIT
-- organization_id so tests can seed a cross-tenant/stale assignment that the
-- org-scoped AddPluginAssignment (INSERT ... SELECT filtered by org) refuses to
-- create.
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
VALUES (@plugin_id, @organization_id, @principal_urn);

-- name: InsertUserFixture :exec
INSERT INTO users (id, email, display_name)
VALUES (@id, @email, @display_name);

-- name: InsertDeviceAgentSyncFixture :exec
INSERT INTO device_agent_syncs (organization_id, email, first_seen_at, last_seen_at)
VALUES (@organization_id, @email, @seen_at, @seen_at);

-- name: ListDeviceAgentDeviceSyncsFixture :many
-- Reads back per-device agent heartbeats so tests can assert the write path;
-- there is no production reader until the coverage join lands.
SELECT organization_id, serial_number, email, hostname, first_seen_at, last_seen_at
FROM device_agent_device_syncs
WHERE organization_id = @organization_id
ORDER BY serial_number ASC;

-- name: InsertMdmDeviceFixture :exec
INSERT INTO mdm_devices (device_integration_config_id, organization_id, external_id, user_email, user_id, serial_number, missing_since)
VALUES (@device_integration_config_id, @organization_id, @external_id, NULLIF(@user_email::text, ''), sqlc.narg('user_id')::text, NULLIF(@serial_number::text, ''), sqlc.narg('missing_since')::timestamptz);

-- name: InsertDeviceAgentDeviceSyncFixture :exec
INSERT INTO device_agent_device_syncs (organization_id, serial_number, email, hostname, first_seen_at, last_seen_at)
VALUES (@organization_id, @serial_number, @email, NULLIF(@hostname::text, ''), @seen_at, @seen_at);

-- name: PauseDeviceIntegrationSyncsFixture :exec
UPDATE device_integration_syncs s
SET auto_paused_at = clock_timestamp(),
    consecutive_failures = 5
FROM device_integration_schedules sch
WHERE s.device_integration_schedule_id = sch.id
  AND sch.device_integration_config_id = @device_integration_config_id;

-- name: DisableDeviceIntegrationSchedulesFixture :exec
UPDATE device_integration_schedules
SET disabled_at = clock_timestamp()
WHERE device_integration_config_id = @device_integration_config_id;

-- Pushes every sync's next poll an hour out, simulating a config whose
-- schedules already ran this interval.
-- name: DeferDeviceIntegrationSyncsFixture :exec
UPDATE device_integration_syncs s
SET next_poll_after = clock_timestamp() + interval '1 hour'
FROM device_integration_schedules sch
WHERE s.device_integration_schedule_id = sch.id
  AND sch.device_integration_config_id = @device_integration_config_id;

-- name: GetDeviceIntegrationCredentialsCiphertext :one
SELECT credentials_encrypted
FROM device_integration_configs
WHERE id = @id;

-- name: FailDeviceIntegrationSyncsFixture :exec
UPDATE device_integration_syncs s
SET last_poll_error = @error_message,
    last_poll_failed_at = clock_timestamp(),
    last_push_digest = @last_push_digest,
    auto_paused_at = clock_timestamp(),
    consecutive_failures = 3
FROM device_integration_schedules sch
WHERE s.device_integration_schedule_id = sch.id
  AND sch.device_integration_config_id = @device_integration_config_id;

-- name: GetDeviceIntegrationSyncPushDigests :many
SELECT s.last_push_digest
FROM device_integration_syncs s
JOIN device_integration_schedules sch
  ON s.device_integration_schedule_id = sch.id
WHERE sch.device_integration_config_id = @device_integration_config_id;

-- name: SetDeviceIntegrationSyncPushDigestFixture :exec
UPDATE device_integration_syncs s
SET last_push_digest = @last_push_digest
FROM device_integration_schedules sch
WHERE s.device_integration_schedule_id = sch.id
  AND sch.device_integration_config_id = @device_integration_config_id;

-- name: CorruptDeviceIntegrationCredentialsFixture :exec
UPDATE device_integration_configs
SET credentials_encrypted = 'not-a-valid-ciphertext'
WHERE id = @id;

-- name: InsertLegacyDenyPrincipalGrantFixture :exec
-- Test-only fixture for exercising allow-only writes against legacy rows.
INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, selectors)
VALUES (@organization_id, @principal_urn, @scope, 'deny', @selectors);

-- name: GetPrincipalGrantEffectFixture :one
SELECT effect
FROM principal_grants
WHERE organization_id = @organization_id
  AND principal_urn = @principal_urn
  AND scope = @scope
  AND selectors = @selectors;

-- name: InsertChatContentPartFixture :one
-- Test-only fixture: seeds a minimal chat content part so tests can anchor a
-- risk_results row to it.
INSERT INTO chat_content_parts (chat_id, project_id, kind, content_asset_url)
VALUES (@chat_id, @project_id, @kind, @content_asset_url)
RETURNING id;

-- name: InsertContentPartRiskResultFixture :exec
-- Test-only fixture: seeds a risk_results row anchored to a chat content part
-- (chat_message_id IS NULL), a shape the production InsertRiskResults copyfrom
-- cannot produce, so backfill tooling can exercise the fallback path its
-- chat_messages join takes when a finding has no chat message.
INSERT INTO risk_results (
  id, project_id, organization_id, risk_policy_id, risk_policy_version,
  chat_content_part_id, source, found, rule_id, description, match, tags
) VALUES (
  @id, @project_id, @organization_id, @risk_policy_id, @risk_policy_version,
  @chat_content_part_id, @source, TRUE, @rule_id, @description, @match, @tags
);

-- name: GetPlatformMCPSetupHandoffHashFixture :one
-- Test-only inspection of the one-way setup credential persisted by Platform MCP.
SELECT handoff_hash
FROM platform_mcp_setup_handoffs
WHERE id = @id;

-- name: ExpirePlatformMCPSetupHandoffFixture :exec
-- Test-only fixture to verify expired setup handoffs cannot be redeemed.
UPDATE platform_mcp_setup_handoffs
SET expires_at = clock_timestamp() - interval '1 second'
WHERE id = @id;

-- name: GetPlatformMCPReadinessFingerprintFixture :one
-- Test-only inspection of the non-secret identity fingerprint persisted by Platform MCP.
SELECT provider_authorization_fingerprint
FROM platform_mcp_readiness
WHERE registration_id = @registration_id
ORDER BY checked_at DESC, id DESC
LIMIT 1;

-- name: CountPlatformMCPSetupMilestoneFixture :one
-- Test-only count for idempotent Platform MCP setup evidence.
SELECT count(*)
FROM platform_mcp_onboarding_milestones
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND mcp_key = @mcp_key
  AND attempt_id = @attempt_id
  AND milestone = @milestone;

-- name: ExpireRemoteSessionAccessTokenFixture :exec
-- Test-only fixture forcing the shared remote-session refresh path.
UPDATE remote_sessions
SET access_expires_at = clock_timestamp() - interval '1 minute'
WHERE id = @id;

-- name: GetToolCallBlockLinksFixture :one
-- Test-only. The block page query deliberately does not expose the optional
-- foreign keys, but asserting that the salvage cleared exactly the link the
-- database rejected — and left the others alone — requires reading them off
-- the row.
SELECT chat_id, chat_message_id, risk_result_id, risk_policy_id
FROM tool_call_blocks
WHERE id = @id;

-- name: CountSkillPromptInjectionResults :one
-- Test-only fixture: counts prompt-injection findings anchored to a version of
-- the named skill. Attribution is what makes the finding useful, so tests
-- assert on the skill_versions join rather than on the finding alone.
SELECT count(*)
FROM risk_results rr
JOIN skill_versions sv ON sv.id = rr.skill_version_id
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.name = @skill_name
  AND rr.source = 'prompt_injection'
  AND rr.found IS TRUE;

-- name: CountSkillScanRecords :one
-- Test-only fixture: counts every recorded scan of a version of the named
-- skill, findings and clean records alike. This is the coverage question -
-- "was this content ever judged" - which CountSkillPromptInjectionResults
-- cannot answer because it filters to found rows.
SELECT count(*)
FROM risk_results rr
JOIN skill_versions sv ON sv.id = rr.skill_version_id
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.name = @skill_name;
