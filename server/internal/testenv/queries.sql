-- name: InsertChatMessage :one
INSERT INTO chat_messages (chat_id, project_id, role, content)
VALUES (@chat_id, @project_id, @role, @content)
RETURNING id;

-- name: InsertKillswitchPrescriptionFixture :exec
WITH fixture_clock AS (
  SELECT clock_timestamp() - INTERVAL '1 hour' AS active_since
),
inserted_prescription AS (
  INSERT INTO killswitch_prescriptions (
    id, organization_id, definition_key, principal_kind, principal_key, resource_kind, current_version
  ) VALUES (
    @prescription_id, @organization_id, @definition_key, @principal_kind, @principal_key, @resource_kind, 1
  )
  RETURNING organization_id, id
),
inserted_version AS (
  INSERT INTO killswitch_prescription_versions (
    organization_id, prescription_id, version, state, resource_scope, starts_at, expires_at, activated_at, internal_note, external_note
  )
  SELECT
    organization_id,
    id,
    1,
    'active',
    CASE
      WHEN @resource_scope::text = 'all' AND cardinality(COALESCE(@resource_keys::text[], ARRAY[]::text[])) = 0 THEN @resource_scope::text
      WHEN @resource_scope::text = 'selected' AND cardinality(COALESCE(@resource_keys::text[], ARRAY[]::text[])) > 0 THEN @resource_scope::text
      ELSE NULL
    END,
    active_since,
    NULL,
    active_since,
    @internal_note,
    @external_note
  FROM inserted_prescription
  CROSS JOIN fixture_clock
  RETURNING organization_id, prescription_id, version
)
INSERT INTO killswitch_prescription_version_resources (
  organization_id, prescription_id, version, resource_key
)
SELECT organization_id, prescription_id, version, resource_key
FROM inserted_version
CROSS JOIN unnest(@resource_keys::text[]) AS resource(resource_key);

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

-- name: CreateRemoteMCPServerMaterializationFailureFunctionFixture :exec
-- Defines the trigger function used to force atomic remote-MCP provisioning to
-- fail after it has created the remote source and session issuer.
CREATE OR REPLACE FUNCTION fail_remote_mcp_server_materialization() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'test materialization failure';
END;
$$ LANGUAGE plpgsql;

-- name: CreateRemoteMCPServerMaterializationFailureTriggerFixture :exec
CREATE TRIGGER fail_remote_mcp_server_materialization
BEFORE INSERT ON mcp_servers
FOR EACH ROW EXECUTE FUNCTION fail_remote_mcp_server_materialization();

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
-- name: SetOpenRouterKeyLifecycleFixture :execrows
-- Test-only fixture for Stripe lifecycle tests.
UPDATE openrouter_api_keys
SET disabled = @disabled,
    disable_causes = @disable_causes,
    monthly_credits = @monthly_credits
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND deleted IS FALSE;

-- name: SeedAuditLogFixture :one
INSERT INTO audit_logs (organization_id, actor_id, actor_type, action, subject_id, subject_type, metadata)
VALUES (@organization_id, 'user:<USER_ID>', 'user', @action, 'subject:<SUBJECT_ID>', 'subject', jsonb_build_object('key_type', @key_type::text))
RETURNING seq;

-- name: SeedUnrelatedAuditHistoryFixture :exec
INSERT INTO audit_logs (organization_id, actor_id, actor_type, action, subject_id, subject_type, metadata)
SELECT @organization_id, 'user:<USER_ID>', 'user', 'unrelated:' || n, 'subject:<SUBJECT_ID>', 'subject', jsonb_build_object('key_type', @key_type::text)
FROM generate_series(1, @event_count::int) AS n;

-- name: InstallOpenRouterAdminDisableAuditFailureFixture :exec
CREATE FUNCTION fail_admin_key_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'forced audit failure';
END
$$;

-- name: EnableOpenRouterAdminDisableAuditFailureFixture :exec
CREATE TRIGGER fail_admin_key_audit
BEFORE INSERT ON audit_logs
FOR EACH ROW
WHEN (NEW.action = 'openrouter-key:disable')
EXECUTE FUNCTION fail_admin_key_audit();

-- name: DisableOpenRouterAdminDisableAuditFailureFixture :exec
ALTER TABLE audit_logs DISABLE TRIGGER fail_admin_key_audit;

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

-- name: ListPublishOutboxRows :many
SELECT id, public_id, organization_id, topic, message, attributes,
       attempts, last_error, retry_after, locked_until, lease_token, created_at
FROM publish_outbox
ORDER BY id;

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
-- disabled state, whitelist flag, trial window, or age. Omit created_at to keep
-- the column default.
INSERT INTO organization_metadata (
    id,
    name,
    slug,
    gram_account_type,
    workos_id,
    whitelisted,
    free_trial_started_at,
    free_trial_ends_at,
    disabled_at,
    created_at
) VALUES (
    @id,
    @name,
    @slug,
    @gram_account_type,
    sqlc.narg('workos_id')::text,
    @whitelisted,
    @free_trial_started_at,
    @free_trial_ends_at,
    sqlc.narg('disabled_at')::timestamptz,
    COALESCE(sqlc.narg('created_at')::timestamptz, clock_timestamp())
);

-- name: SetWorkosLastEventIDFixture :exec
-- Test-only fixture for seeding the WorkOS webhook cursor on an organization
-- that already exists. Deliberately kept out of
-- CreateOrganizationMetadataFixture: several branches add columns to that
-- INSERT at once, and a column added mid-list renumbers every positional
-- placeholder after it in the generated code, which a hand-resolved merge can
-- get wrong while still compiling.
UPDATE organization_metadata
SET workos_last_event_id = @workos_last_event_id
WHERE id = @id;

-- name: GetOrganizationMetadataStateFixture :one
-- Test-only fixture for asserting what a write to organization_metadata did
-- and did not touch. disabled_at comes back at full precision: the admin API
-- renders it as a second-resolution RFC3339 string, which hides a timestamp
-- that moved by microseconds. workos_last_event_id is the WorkOS webhook
-- cursor, which only the webhook path may write. created_at and updated_at are
-- the reference points for "did this write stamp the moment of the action":
-- comparing a stamp against them keeps the comparison inside the database
-- clock, which the test host's clock can drift from.
-- gram_account_type and whitelisted are the two columns trial demotion drops,
-- so a write that only extends a trial has to leave both exactly where it found
-- them.
SELECT disabled_at, workos_last_event_id, whitelisted, gram_account_type, created_at, updated_at
FROM organization_metadata
WHERE id = @id;

-- name: CountOrganizationsForWorkosIDFixture :one
-- Test-only fixture for proving that two writers converged on one row instead
-- of creating two. Every read the API offers returns at most one organization,
-- so a duplicate row is invisible through it and only a count can see it.
SELECT count(*)
FROM organization_metadata
WHERE workos_id = @workos_id::text;

-- name: CreateOrganizationUserRelationshipFixture :exec
-- Test-only fixture for seeding membership counts.
INSERT INTO organization_user_relationships (organization_id, user_id)
VALUES (@organization_id, sqlc.narg('user_id')::text);

-- name: ForceSoftDeleteOrganizationUserRelationshipsFixture :exec
-- Test-only fixture for seeding a removed member. The deleted column is
-- generated from deleted_at, so a soft delete has to set the timestamp.
UPDATE organization_user_relationships
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id;

-- name: ForceSoftDeleteUserSessionIssuer :exec
-- Test-only fixture for defensive paths that handle a dangling soft-delete FK.
UPDATE user_session_issuers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: SetUserSessionIssuerCIMDAdmissionMode :exec
-- Test-only fixture: writes an issuer's CIMD admission mode as a single-column
-- update. The production UpdateUserSessionIssuer query COALESCEs every param,
-- where a Valid-but-empty pgtype.Text silently clobbers the stored value;
-- keeping that contract out of per-package test helpers is the point of this
-- narrow query.
UPDATE user_session_issuers
SET client_id_metadata_admission_mode = @client_id_metadata_admission_mode
WHERE id = @id AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: SetUserSessionIssuerOrganizationID :exec
-- Test-only fixture: repoints an issuer's organization so tests can observe
-- what a child row does when its parent's tenancy no longer matches its own.
-- No production path moves an issuer between organizations yet, so there is
-- no other way to reach that state.
UPDATE user_session_issuers
SET organization_id = @organization_id
WHERE id = @id AND project_id = @project_id::uuid AND deleted IS FALSE;

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

-- name: SetRemoteSessionResourceFixture :exec
-- Test-only fixture stamping a stored RFC 8707 resource binding on a row.
UPDATE remote_sessions
SET resource = @resource
WHERE subject_urn = @subject_urn
  AND remote_session_client_id = @remote_session_client_id;

-- name: GetToolCallBlockLinksFixture :one
-- Test-only. The block page query deliberately does not expose the optional
-- foreign keys, but asserting that the salvage cleared exactly the link the
-- database rejected — and left the others alone — requires reading them off
-- the row.
SELECT chat_id, chat_message_id, risk_result_id, risk_policy_id
FROM tool_call_blocks
WHERE id = @id;

-- name: CountSkillScanRecords :one
-- Test-only fixture: counts recorded scans of a version of the named skill.
-- found_only narrows the count to prompt-injection findings; otherwise every
-- recorded scan counts, clean coverage rows included.
SELECT count(*)
FROM risk_results rr
JOIN skill_versions sv ON sv.id = rr.skill_version_id
JOIN skills s ON s.id = sv.skill_id
WHERE s.project_id = @project_id
  AND s.name = @skill_name
  AND (
    NOT @found_only::boolean
    OR (rr.source = 'prompt_injection' AND rr.found IS TRUE)
  );

-- name: ForceSoftDeleteUser :exec
-- Test-only fixture: soft-deletes a directory user to exercise deleted-row
-- filtering in identity resolution.
UPDATE users
SET deleted_at = clock_timestamp()
WHERE id = @id;

-- name: ForceSoftDeleteOrganizationUserRelationship :exec
-- Test-only fixture: soft-deletes an org membership to exercise deleted-row
-- filtering in identity resolution.
UPDATE organization_user_relationships
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND user_id = @user_id;

-- name: ForceSoftDeleteUserAccountsByEmail :exec
-- Test-only fixture: soft-deletes an org's linked accounts by email to
-- exercise deleted-row filtering in identity resolution.
UPDATE user_accounts
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND lower(email) = @email_lower::text;

-- name: GetTransactionClockFixture :one
-- Test-only. Returns the transaction timestamp and the two edges a 7-day
-- INTERVAL predicate compares against, so a boundary fixture can be seeded
-- exactly on an edge.
--
-- Postgres computes the offsets rather than the test, because INTERVAL day
-- arithmetic on timestamptz runs in the session time zone and need not come to
-- 168 hours. It must be read inside the transaction that also runs the query
-- under test: now() is the transaction timestamp, so reading it on its own
-- connection puts the fixture on an edge that has already moved.
SELECT
    now()::timestamptz AS transaction_now,
    (now() - INTERVAL '7 days')::timestamptz AS seven_days_ago,
    (now() + INTERVAL '7 days')::timestamptz AS in_seven_days;

-- name: SetOpenRouterAPIKeyCreatedAtFixture :exec
-- Test-only fixture: places a platform-managed key before a historical spend
-- range so completeness checks expect every day in that range.
UPDATE openrouter_api_keys
SET created_at = @created_at
WHERE organization_id = @organization_id;

-- name: SetOpenRouterAPIKeyClassificationFixture :exec
-- Test-only fixture: creates compatibility states that production writes reject.
UPDATE openrouter_api_keys
SET disabled = @disabled,
    disable_causes = @disable_causes::text[]
WHERE organization_id = @organization_id
  AND key_type = @key_type;

-- name: SetOpenRouterAPIKeyHashFixture :exec
-- Test-only fixture: simulates key rotation between an upstream response and CAS.
UPDATE openrouter_api_keys
SET key_hash = @key_hash
WHERE organization_id = @organization_id
  AND key_type = @key_type;

-- name: SeedRearmAuditMetadataFixture :exec
-- Test-only fixture: seeds a historical re-arm audit with caller-provided metadata.
INSERT INTO audit_logs (organization_id, actor_id, actor_type, action, subject_id, subject_type, metadata)
VALUES (@organization_id, 'system', 'user', 'organization:enterprise_trial_rearmed', @organization_id, 'organization', @metadata::jsonb);

-- name: IsQueryBlockedOnLockFixture :one
-- Test-only synchronization: reports whether a matching active query is waiting on a lock.
SELECT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_stat_activity
    WHERE datname = current_database()
      AND state = 'active'
      AND wait_event_type = 'Lock'
      AND query LIKE @query_pattern::text
);

-- name: TryAcquireOpenRouterKeyBillingLockFixture :one
-- Test-only non-blocking probe of the production OpenRouter billing lock key.
SELECT pg_try_advisory_lock(
    hashtextextended('openrouter-' || @key_type::text || '-billing:' || @organization_id::text, 0)
);

-- name: SoftDeleteOpenRouterAPIKeyFixture :exec
-- Test-only fixture: soft-deletes one classified key row.
UPDATE openrouter_api_keys
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND key_type = @key_type;

-- name: LockOpenRouterAPIKeyForUpdateFixture :one
-- Test-only synchronization: holds a row lock even when the key is soft-deleted.
SELECT 1
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND key_type = @key_type
FOR UPDATE;

-- name: ListOpenRouterAPIKeyDisableCausesForUpdateNowaitFixture :many
-- Test-only lock-order probe: fails immediately if any matching key row is locked.
SELECT disable_causes
FROM openrouter_api_keys
WHERE organization_id = @organization_id
  AND deleted IS FALSE
ORDER BY key_type
FOR UPDATE NOWAIT;

-- name: SeedOpenRouterSpendRangeFixture :exec
-- Test-only fixture: records one exact daily spend amount across an inclusive
-- UTC date range.
INSERT INTO openrouter_spend_daily (organization_id, key_type, day, spend_usd)
SELECT
    sqlc.arg(organization_id)::text
  , sqlc.arg(key_type)::text
  , day::date
  , sqlc.arg(spend_usd)::text::numeric(14, 6)
FROM GENERATE_SERIES(sqlc.arg(start_day)::date, sqlc.arg(end_day)::date, INTERVAL '1 day') AS day;

-- name: DeleteOpenRouterSpendDayFixture :exec
-- Test-only fixture: creates an incomplete historical month.
DELETE FROM openrouter_spend_daily
WHERE organization_id = @organization_id
  AND key_type = @key_type
  AND day = @day;

-- name: GetSessionHandoffLinkFixture :one
-- Test-only inspection of a minted session-handoff link, so tests can assert a
-- consumed link keeps its burn bookkeeping without keeping the blob pointer.
SELECT blob_url, consumed_at
FROM session_handoff_links
WHERE token = @token;

-- name: SeedCapturedAgentChatFixture :one
-- Test-only fixture: inserts the chat row a captured agent session hangs off,
-- with the harness-native session id stored as external_chat_id and an
-- optional personal/team account attribution.
INSERT INTO chats (id, project_id, organization_id, user_id, external_chat_id, title, cwd, user_account_id)
VALUES (@id, @project_id, @organization_id, @user_id, sqlc.narg(external_chat_id), @title, sqlc.narg(cwd), sqlc.narg(user_account_id))
RETURNING id;

-- name: SeedCapturedAgentChatMessageFixture :one
-- Test-only fixture: inserts a captured transcript row with the full recall
-- shape — generation, tool_calls, capture source, asset offload marker, and
-- risk-analysis completion — at a deterministic created_at.
INSERT INTO chat_messages (chat_id, project_id, role, content, generation, tool_calls, source, content_asset_url, risk_analyzed_at, created_at)
VALUES (@chat_id, @project_id, @role, @content, @generation, sqlc.narg(tool_calls), sqlc.narg(source), sqlc.narg(content_asset_url), sqlc.narg(risk_analyzed_at), @created_at)
RETURNING id;

-- name: SeedUserAccountFixture :one
-- Test-only fixture: inserts a minimal provider account row so chats can be
-- attributed to a team or personal account.
INSERT INTO user_accounts (organization_id, external_account_uuid, account_type)
VALUES (@organization_id, @external_account_uuid, @account_type)
RETURNING id;

-- name: SeedRiskPolicyFixture :one
-- Test-only fixture: inserts an enabled standard risk policy.
INSERT INTO risk_policies (project_id, organization_id, name, sources, version)
VALUES (@project_id, @organization_id, @name, @sources, 1)
RETURNING id;

-- name: SeedRiskResultFixture :one
-- Test-only fixture: records one open finding against a chat message, with the
-- primary span mirrored into the spans JSONB set.
INSERT INTO risk_results (project_id, organization_id, risk_policy_id, risk_policy_version, chat_message_id, source, found, rule_id, match, start_pos, end_pos, spans)
VALUES (@project_id, @organization_id, @risk_policy_id, 1, @chat_message_id, @source, TRUE, @rule_id, @match, @start_pos, @end_pos, sqlc.narg(spans))
RETURNING id;

-- name: GetChatSessionLinkByParentFixture :one
-- Test-only inspection of a recorded session-lineage edge from its parent end.
SELECT kind, child_chat_id, parent_session_id, target_harness, organization_id, project_id
FROM chat_session_links
WHERE parent_chat_id = @parent_chat_id;

-- name: CountChatSessionLinksByKindFixture :one
SELECT COUNT(*)
FROM chat_session_links
WHERE parent_chat_id = @parent_chat_id
  AND kind = @kind;
-- name: ForceSoftDeleteRemoteSessionIssuerFixture :exec
-- Tombstones a remote session issuer regardless of its clients. Production
-- deletes refuse while a live client references it, so this is the only way to
-- build the state the derivation must reject.
UPDATE remote_session_issuers
SET deleted_at = clock_timestamp()
WHERE id = @id;

-- name: SetMCPServerRemoteSessionIssuerFixture :execrows
-- Test-only fixture: stamps the denormalised upstream authorization server on
-- an MCP server. Server creation cannot set it — no client bindings exist yet —
-- so tests seed it after the fact, standing in for the binding resync.
--
-- Returns the row count so the caller can insist the stamp landed: one that
-- matched nothing would otherwise let a negative test pass vacuously.
UPDATE mcp_servers
SET remote_session_issuer_id = @remote_session_issuer_id
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;
