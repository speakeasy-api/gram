-- Postflight asserts: demo data landed, and nothing leaked outside the demo
-- org. Each query returns a count (or a pass/fail flag) that postflightCore
-- checks in Go.

-- name: PostflightCountInferenceHookMessages :one
SELECT count(*) FROM chat_messages
WHERE project_id = @project_id::uuid AND chat_messages.chat_id = @chat_id::uuid
  AND source = 'claude-chat';

-- name: PostflightCountChats :one
SELECT count(*) FROM chats WHERE organization_id = @org_id::text;

-- name: PostflightCountFindings :one
SELECT count(*)
FROM risk_results WHERE organization_id = @org_id::text AND risk_results.found;

-- name: PostflightCountMembers :one
SELECT count(*)
FROM organization_user_relationships WHERE organization_id = @org_id::text AND deleted_at IS NULL;

-- name: PostflightCountSetupTasks :one
SELECT count(*) FROM organization_setup_tasks
WHERE organization_id = @org_id::text;

-- name: PostflightSecurityOnboardingInvalid :one
SELECT (
  (SELECT preset FROM organization_onboarding WHERE organization_id = @org_id::text) IS DISTINCT FROM 'security'
  OR (SELECT count(*) FROM organization_setup_tasks WHERE organization_id = @org_id::text AND hidden_at IS NULL) <> 9
  OR EXISTS (SELECT 1 FROM organization_setup_tasks WHERE organization_id = @org_id::text
    AND ((task_key IN ('anthropic-admin-controls', 'platform-mcp')) IS DISTINCT FROM (hidden_at IS NOT NULL)))
)::boolean AS invalid;

-- name: PostflightOnboardingStackInvalid :one
SELECT (
  (SELECT mdm_vendor FROM organization_onboarding WHERE organization_id = @org_id::text) IS DISTINCT FROM 'jamf'
  OR (SELECT count(*) FROM organization_onboarding_vendors WHERE organization_id = @org_id::text) <> 2
)::boolean AS invalid;

-- name: PostflightCountNetworkIngressFeature :one
SELECT count(*) FROM organization_features
WHERE organization_id = @org_id::text AND feature_name = 'network_ingress';

-- name: PostflightCountNetworkIngresses :one
SELECT count(*) FROM network_ingresses
WHERE organization_id = @org_id::text;

-- name: PostflightCountHTTPTools :one
SELECT count(*)
FROM http_tool_definitions WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- Killswitch aggregate counts are exact: six headers, two successor
-- versions, eight complete selected snapshots, one expiry marker, and one
-- completed operation/audit event for every lifecycle mutation.

-- name: PostflightCountKillswitchPrescriptions :one
SELECT count(*) FROM killswitch_prescriptions
WHERE organization_id = @org_id::text;

-- name: PostflightCountKillswitchVersions :one
SELECT count(*) FROM killswitch_prescription_versions
WHERE organization_id = @org_id::text;

-- name: PostflightCountKillswitchResources :one
SELECT count(*) FROM killswitch_prescription_version_resources
WHERE organization_id = @org_id::text;

-- name: PostflightCountKillswitchOperations :one
SELECT count(*) FROM killswitch_operations
WHERE organization_id = @org_id::text;

-- name: PostflightCountKillswitchExpiryEvents :one
SELECT count(*) FROM killswitch_expiry_events
WHERE organization_id = @org_id::text;

-- current_version always names the newest immutable version. Only historical
-- versions are superseded, and every version retains its activation time.

-- name: PostflightCountKillswitchInvalidCurrentVersion :one
SELECT count(*)
FROM killswitch_prescriptions p
LEFT JOIN killswitch_prescription_versions current_v
  ON current_v.organization_id = p.organization_id
 AND current_v.prescription_id = p.id
 AND current_v.version = p.current_version
WHERE p.organization_id = @org_id::text
  AND (current_v.prescription_id IS NULL
    OR current_v.version <> (
      SELECT max(v.version) FROM killswitch_prescription_versions v
      WHERE v.organization_id = p.organization_id AND v.prescription_id = p.id
    ));

-- name: PostflightCountKillswitchInvalidLifecycle :one
SELECT count(*)
FROM killswitch_prescription_versions v
JOIN killswitch_prescriptions p
  ON p.organization_id = v.organization_id AND p.id = v.prescription_id
WHERE v.organization_id = @org_id::text
  AND (v.activated_at IS NULL
    OR (v.version = p.current_version AND v.superseded_at IS NOT NULL)
    OR (v.version < p.current_version AND v.superseded_at IS NULL));

-- Immediate versions have no explicit start; the only non-NULL start is the
-- future scheduled window. Every version has a historical activation time.

-- name: PostflightKillswitchStartSemanticsInvalid :one
SELECT (CASE WHEN
    count(*) FILTER (WHERE starts_at IS NULL) = 7
    AND count(*) FILTER (WHERE starts_at > clock_timestamp()) = 1
    AND count(*) FILTER (WHERE starts_at IS NOT NULL) = 1
    AND count(*) FILTER (
      WHERE activated_at IS NOT NULL AND activated_at < clock_timestamp()
    ) = 8
  THEN 0 ELSE 1 END)::bigint AS stray
FROM killswitch_prescription_versions
WHERE organization_id = @org_id::text;

-- An all-resource snapshot has no children. Every selected version has a
-- complete non-empty snapshot, including copied snapshots on lift.

-- name: PostflightCountKillswitchInvalidResourceCardinality :one
SELECT count(*)
FROM killswitch_prescription_versions v
LEFT JOIN LATERAL (
  SELECT count(*) AS resource_count
  FROM killswitch_prescription_version_resources r
  WHERE r.organization_id = v.organization_id
    AND r.prescription_id = v.prescription_id
    AND r.version = v.version
) resources ON TRUE
WHERE v.organization_id = @org_id::text
  AND ((v.resource_scope = 'all' AND resources.resource_count <> 0)
    OR (v.resource_scope = 'selected' AND resources.resource_count = 0));

-- Principal and resource keys are canonical records owned by this org.

-- name: PostflightCountKillswitchInvalidPrincipal :one
SELECT count(*)
FROM killswitch_prescriptions p
WHERE p.organization_id = @org_id::text
  AND (p.definition_key <> 'mcp_tool_execution'
    OR p.principal_kind <> 'user'
    OR p.resource_kind <> 'mcp_server'
    OR NOT EXISTS (
      SELECT 1 FROM organization_user_relationships member
      WHERE member.organization_id = p.organization_id
        AND member.user_id = p.principal_key
        AND member.deleted_at IS NULL
    ));

-- name: PostflightCountKillswitchInvalidResources :one
SELECT count(*)
FROM killswitch_prescription_version_resources r
LEFT JOIN mcp_servers server ON server.id::text = r.resource_key
LEFT JOIN projects project ON project.id = server.project_id
WHERE r.organization_id = @org_id::text
  AND (server.id IS NULL OR server.deleted IS TRUE
    OR project.organization_id IS DISTINCT FROM @org_id::text);

-- Database-time status projection: three active, one scheduled, one lifted,
-- and one expired current aggregate. The two Amara rows overlap because one
-- selected-server interval intersects one dynamic all-server interval.

-- name: PostflightKillswitchStatusCoverageInvalid :one
SELECT (CASE WHEN
    count(*) FILTER (WHERE customer_status = 'active') = 3
    AND count(*) FILTER (WHERE customer_status = 'scheduled') = 1
    AND count(*) FILTER (WHERE customer_status = 'lifted') = 1
    AND count(*) FILTER (WHERE customer_status = 'expired') = 1
  THEN 0 ELSE 1 END)::bigint AS stray
FROM (
  SELECT CASE
    WHEN v.state = 'inactive' THEN 'lifted'
    WHEN v.starts_at > clock_timestamp() THEN 'scheduled'
    WHEN v.expires_at IS NOT NULL AND v.expires_at <= clock_timestamp() THEN 'expired'
    ELSE 'active'
  END AS customer_status
  FROM killswitch_prescriptions p
  JOIN killswitch_prescription_versions v
    ON v.organization_id = p.organization_id
   AND v.prescription_id = p.id
   AND v.version = p.current_version
  WHERE p.organization_id = @org_id::text
) statuses;

-- name: PostflightCountKillswitchOverlappingActive :one
SELECT count(*)
FROM killswitch_prescriptions p
JOIN killswitch_prescription_versions v
  ON v.organization_id = p.organization_id
 AND v.prescription_id = p.id
 AND v.version = p.current_version
WHERE p.organization_id = @org_id::text
  AND p.principal_key = @principal_key::text
  AND v.state = 'active'
  AND (v.expires_at IS NULL OR clock_timestamp() < v.expires_at)
  AND (v.resource_scope = 'all' OR EXISTS (
    SELECT 1 FROM killswitch_prescription_version_resources r
    WHERE r.organization_id = v.organization_id
      AND r.prescription_id = v.prescription_id
      AND r.version = v.version
      AND r.resource_key = @resource_key::text
  ));

-- Completed replay receipts use the production response envelope, point to
-- the exact committed version/state, expire 30 days after claim, and belong
-- to a current organization member.

-- name: PostflightCountKillswitchInvalidOperations :one
SELECT count(*)
FROM killswitch_operations operation
LEFT JOIN killswitch_prescription_versions version
  ON version.organization_id = operation.organization_id
 AND version.prescription_id::text = operation.response ->> 'prescription_id'
 AND version.version = (operation.response ->> 'prescription_version')::bigint
WHERE operation.organization_id = @org_id::text
  AND (operation.status <> 'completed'
    OR operation.response ->> 'response_version' <> 'killswitch-operation-response-v1'
    OR operation.request_hash !~ '^sha256:[0-9a-f]{64}$'
    OR operation.expires_at <> operation.created_at + interval '30 days'
    OR version.prescription_id IS NULL
    OR version.state <> operation.response ->> 'state'
    OR NOT EXISTS (
      SELECT 1 FROM organization_user_relationships member
      WHERE member.organization_id = operation.organization_id
        AND member.user_id = operation.actor_user_id
        AND member.deleted_at IS NULL
    ));

-- Every mutation has one canonical audit action joined by operation_id. The
-- expiry marker has one separate history-only expiry event.

-- name: PostflightCountKillswitchLifecycleAudits :one
SELECT count(*) FROM audit_logs
WHERE organization_id = @org_id::text
  AND subject_type = 'killswitch_prescription'
  AND action IN ('killswitch:activate', 'killswitch:change', 'killswitch:deactivate');

-- name: PostflightCountKillswitchInvalidLifecycleAudits :one
SELECT count(*)
FROM audit_logs event
LEFT JOIN killswitch_operations operation
  ON operation.organization_id = event.organization_id
 AND operation.operation_id::text = event.metadata ->> 'operation_id'
LEFT JOIN killswitch_prescription_versions version
  ON version.organization_id = event.organization_id
 AND version.prescription_id::text = event.subject_id
 AND version.version = (event.after_snapshot ->> 'version')::bigint
WHERE event.organization_id = @org_id::text
  AND event.subject_type = 'killswitch_prescription'
  AND event.action IN ('killswitch:activate', 'killswitch:change', 'killswitch:deactivate')
  AND (event.project_id IS NOT NULL
    OR operation.operation_id IS NULL
    OR event.action <> 'killswitch:' || CASE operation.operation
      WHEN 'activate' THEN 'activate' WHEN 'change' THEN 'change'
      WHEN 'deactivate' THEN 'deactivate' ELSE 'invalid' END
    OR event.metadata ->> 'operation' <> operation.operation
    OR version.prescription_id IS NULL
    OR version.state <> event.after_snapshot ->> 'state'
    OR operation.response ->> 'prescription_id' <> event.subject_id
    OR operation.response ->> 'prescription_version' <> event.after_snapshot ->> 'version');

-- name: PostflightCountKillswitchExpiryAudits :one
SELECT count(*)
FROM audit_logs event
JOIN killswitch_expiry_events expiry
  ON expiry.organization_id = event.organization_id
 AND expiry.prescription_id::text = event.subject_id
 AND expiry.version = (event.metadata ->> 'version')::bigint
JOIN killswitch_prescription_versions version
  ON version.organization_id = expiry.organization_id
 AND version.prescription_id = expiry.prescription_id
 AND version.version = expiry.version
WHERE event.organization_id = @org_id::text
  AND event.subject_type = 'killswitch_prescription'
  AND event.action = 'killswitch:expire'
  AND event.project_id IS NULL
  AND event.actor_id = 'system'
  AND event.actor_type = 'user'
  AND event.actor_display_name = 'System'
  AND event.after_snapshot IS NULL
  AND (event.metadata ->> 'expired_at')::timestamptz = version.expires_at
  AND expiry.recorded_at > version.expires_at;

-- name: PostflightCountShadowPolicyGrants :one
SELECT count(*)
FROM principal_grants
WHERE organization_id = @org_id::text
  AND scope = 'risk_policy:bypass'
  AND selectors ->> 'resource_id' = @policy_id::text
  AND selectors ->> 'server_url' = 'https://shadow-mcp.demo.getgram.ai/research';

-- The Watchdog scores each signal from its findings' policy, so a rotation
-- that collapsed onto one policy would render every signal at one severity.

-- name: PostflightCountFindingPolicies :one
SELECT count(DISTINCT risk_policy_id)
FROM risk_results WHERE organization_id = @org_id::text AND risk_results.found;

-- Suppressed findings back the exclusion and Dismissed surfaces. Each
-- mechanism must be represented, or a broken rendering path stays invisible.

-- name: PostflightCountExcludedFindings :one
SELECT count(*)
FROM risk_results WHERE organization_id = @org_id::text AND excluded_exclusion_id IS NOT NULL;

-- name: PostflightCountDismissedFindings :one
SELECT count(*)
FROM risk_results WHERE organization_id = @org_id::text AND false_positive_at IS NOT NULL;

-- An exclusion whose match_value no finding carries would make the
-- suppressed rows above untraceable to the rule that claims them.

-- name: PostflightCountIdleExactExclusions :one
SELECT count(*) FROM risk_exclusions e
WHERE e.organization_id = @org_id::text AND e.deleted IS FALSE
  AND e.match_type = 'exact'
  AND NOT EXISTS (
    SELECT 1 FROM risk_results r
    WHERE r.organization_id = @org_id::text AND r.excluded_exclusion_id = e.id
      AND r.match = e.match_value);

-- Both checks guard an opaque string that fails silently: an unrecognized
-- source disables scanning for the policy ('regex' shipped that way once),
-- and a custom_rule_id with no rule row makes the policy a no-op. The source
-- list mirrors validateSources (internal/risk/impl.go); the seed inserts
-- past it.

-- name: PostflightCountPoliciesUnknownSource :one
SELECT count(*) FROM risk_policies p
WHERE p.organization_id = @org_id::text AND p.deleted IS FALSE
  AND EXISTS (
    SELECT 1 FROM unnest(p.sources) s
    WHERE s <> ALL (ARRAY['gitleaks', 'presidio', 'shadow_mcp', 'destructive_tool',
                          'cli_destructive', 'prompt_injection', 'account_identity']));

-- name: PostflightCountPolicyMissingCustomRules :one
SELECT count(*)
FROM risk_policies p, unnest(p.custom_rule_ids) cid
WHERE p.organization_id = @org_id::text AND p.deleted IS FALSE
  AND NOT EXISTS (
    SELECT 1 FROM risk_custom_detection_rules r
    WHERE r.project_id = p.project_id AND r.rule_id = cid AND r.deleted IS FALSE);

-- name: PostflightCountForeignOrgChats :one
SELECT count(*)
FROM chats WHERE project_id = @project_id::uuid AND organization_id <> @org_id::text;

-- name: PostflightCountOwnerlessChats :one
SELECT count(*)
FROM chats WHERE organization_id = @org_id::text
  AND (user_id IS NULL OR user_id = '' OR external_user_id IS NULL);

-- name: PostflightCountAgentAttributedChats :one
-- Channel sessions carry the agent that produced them; without that stamp the
-- session list cannot name an actor for them at all, because they have no
-- assistant record. The join to agents is what proves the stamp still resolves:
-- an id pointing at an agent the seed no longer creates renders as a bare id.
SELECT count(*)
FROM chats c
JOIN agents a ON a.id = c.agent_id AND a.organization_id = c.organization_id
  AND a.deleted IS FALSE
WHERE c.project_id = @project_id::uuid AND c.organization_id = @org_id::text;

-- Every table that carries both the demo org id and a project id must agree
-- with the demo constants: a mismatch means an insert escaped its scoping.

-- name: PostflightCountForeignProjectRows :one
SELECT count(*) FROM (
  SELECT 1 FROM deployments WHERE organization_id = @org_id::text AND project_id <> @project_id::uuid
  UNION ALL
  SELECT 1 FROM toolsets WHERE organization_id = @org_id::text AND project_id <> @project_id::uuid
  UNION ALL
  SELECT 1 FROM risk_policies WHERE organization_id = @org_id::text AND project_id <> @project_id::uuid
  UNION ALL
  SELECT 1 FROM risk_results WHERE organization_id = @org_id::text AND project_id <> @project_id::uuid
  UNION ALL
  SELECT 1 FROM risk_exclusions WHERE organization_id = @org_id::text AND project_id <> @project_id::uuid
  UNION ALL
  SELECT 1 FROM session_quarantines WHERE organization_id = @org_id::text AND project_id <> @project_id::uuid
  UNION ALL
  SELECT 1 FROM audit_logs WHERE organization_id = @org_id::text AND project_id <> @project_id::uuid
) x;

-- The gateway is only a gateway if its members survived the reseed, and an
-- endpoint is what gives it a URL — without either the MCP pages render it
-- as an empty shell.

-- name: PostflightCountGatewayMembers :one
SELECT count(*)
FROM meta_mcp_server_members m
WHERE m.project_id = @project_id::uuid AND m.deleted IS FALSE;

-- name: PostflightCountRemovedGatewayMembers :one
SELECT count(*)
FROM meta_mcp_server_members m
WHERE m.project_id = @project_id::uuid AND m.deleted IS TRUE;

-- name: PostflightCountUnwrappedToolsets :one
SELECT count(*)
FROM toolsets t
LEFT JOIN mcp_servers s
  ON s.id = t.id AND s.toolset_id = t.id AND s.deleted IS FALSE
 AND s.slug = t.mcp_slug AND s.name = t.name
 AND s.user_session_issuer_id IS NOT DISTINCT FROM t.user_session_issuer_id
 AND s.tool_variations_group_id IS NOT DISTINCT FROM t.tool_variations_group_id
 AND s.visibility = CASE WHEN NOT t.mcp_enabled THEN 'disabled'
                         WHEN t.mcp_is_public THEN 'public'
                         ELSE 'private' END
WHERE t.project_id = @project_id::uuid AND t.deleted IS FALSE AND t.mcp_slug IS NOT NULL
  AND (s.id IS NULL
    OR 1 <> (SELECT count(*) FROM mcp_endpoints e
             WHERE e.mcp_server_id = t.id AND e.deleted IS FALSE)
    OR NOT EXISTS (
      SELECT 1 FROM mcp_endpoints e
      WHERE e.mcp_server_id = t.id AND e.deleted IS FALSE
        AND e.slug = t.mcp_slug
        AND e.custom_domain_id IS NOT DISTINCT FROM t.custom_domain_id));

-- name: PostflightCountGatewayEndpoints :one
SELECT count(*)
FROM mcp_endpoints e
WHERE e.project_id = @project_id::uuid AND e.deleted IS FALSE
  AND e.meta_mcp_server_id IS NOT NULL;

-- A member whose server lost its backend can never be dispatched to, so it
-- would sit in the gateway's table permanently unavailable.

-- name: PostflightCountUnservableGatewayMembers :one
SELECT count(*)
FROM meta_mcp_server_members m
JOIN mcp_servers s ON s.id = m.mcp_server_id
WHERE m.project_id = @project_id::uuid AND m.deleted IS FALSE
  AND (s.deleted IS TRUE OR s.slug IS NULL
       OR num_nonnulls(s.toolset_id, s.remote_mcp_server_id,
                       s.tunneled_mcp_server_id, s.unproxied_mcp_server_id) <> 1);

-- name: PostflightCountTunnelEndpoints :one
SELECT count(*)
FROM mcp_endpoints e
JOIN mcp_servers s ON s.id = e.mcp_server_id
WHERE e.project_id = @project_id::uuid AND e.deleted IS FALSE
  AND s.tunneled_mcp_server_id IS NOT NULL;

-- name: PostflightCountActiveQuarantines :one
SELECT count(*) FROM session_quarantines
WHERE organization_id = @org_id::text AND project_id = @project_id::uuid AND released_at IS NULL;

-- name: PostflightCountQuarantineOpenAudits :one
SELECT count(*)
FROM audit_logs a
JOIN session_quarantines q
  ON q.id::text = a.subject_id
 AND q.organization_id = a.organization_id
 AND q.project_id = a.project_id
WHERE a.organization_id = @org_id::text
  AND a.project_id = @project_id::uuid
  AND a.action = 'session_quarantine:open'
  AND a.subject_type = 'session_quarantine'
  AND a.subject_slug = q.session_id
  AND a.metadata ->> 'session_id' = q.session_id;

-- Global (non-org-scoped) tables must only carry rows for the demo roster:
-- memberships/accounts/devices reference the roster's user ids exclusively.
-- user_like is the roster's escaped user id prefix plus '%'.

-- name: PostflightCountForeignUserRows :one
SELECT count(*) FROM (
  SELECT 1 FROM organization_user_relationships
  WHERE organization_id = @org_id::text AND user_id NOT LIKE @user_like::text
  UNION ALL
  SELECT 1 FROM user_accounts
  WHERE organization_id = @org_id::text AND user_id NOT LIKE @user_like::text
  UNION ALL
  SELECT 1 FROM device_owners
  WHERE organization_id = @org_id::text AND linked_user_id NOT LIKE @user_like::text
  UNION ALL
  SELECT 1 FROM directory_users
  WHERE organization_id = @org_id::text AND user_id NOT LIKE @user_like::text
  UNION ALL
  -- A device may legitimately resolve to nobody (the unresolved-email
  -- bucket); one that resolves to somebody must resolve to the roster.
  SELECT 1 FROM mdm_devices
  WHERE organization_id = @org_id::text AND user_id IS NOT NULL
    AND user_id NOT LIKE @user_like::text
) x;

-- The MDM fleet: seven devices under one integration. Counted because the
-- coverage buckets are the point — a rerun that dropped the unresolved or
-- agentless rows would leave the widgets showing a uniformly healthy fleet.

-- name: PostflightCountMDMDevices :one
SELECT count(*) FROM mdm_devices WHERE organization_id = @org_id::text;

-- name: PostflightCountDeviceIntegrationConfigs :one
SELECT count(*) FROM device_integration_configs
WHERE organization_id = @org_id::text AND deleted IS FALSE;

-- Personal accounts are the reading the governance note exists for; one
-- surviving row would make the pattern look like an edge case.

-- name: PostflightCountPersonalAccounts :one
SELECT count(*) FROM user_accounts
WHERE organization_id = @org_id::text AND account_type = 'personal';

-- name: PostflightCountRegistryPromotionCandidates :one
SELECT count(*) FROM registry_promotion_candidates
WHERE organization_id = @org_id::text AND project_id = @project_id::uuid
 AND eligible IS FALSE AND promoted_at IS NULL ;

-- name: PostflightCountAuthClientAssociations :one
SELECT count(*) FROM mcp_server_auth_clients
WHERE organization_id = @org_id::text AND deleted IS FALSE;
