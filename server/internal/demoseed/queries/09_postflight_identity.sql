-- Postflight asserts, second half (postgres.sql lines 3693-4302): identity,
-- connections, workloads, Slack, widgets, and sigint. Each query is one
-- count the Go code in seed_09_postflight_identity.go checks.

-- The seed never creates API keys: any row left here was minted by a demo
-- visitor and would grant programmatic access that survives the reseed.
-- name: PostflightIDAPIKeys :one
SELECT count(*) FROM api_keys WHERE organization_id = @org_id::text;

-- name: PostflightIDAgentGrants :one
SELECT count(*) FROM principal_grants
WHERE organization_id = @org_id::text AND principal_urn LIKE 'agent:%';

-- name: PostflightIDScopedAgentGrants :one
SELECT count(*) FROM principal_grants
WHERE organization_id = @org_id::text
  AND principal_urn IN (@agent_urn_1::text, @agent_urn_4::text)
  AND scope = 'mcp:connect'
  AND selectors = jsonb_build_object(
    'resource_kind', 'mcp',
    'resource_id', @mcp_server_id::text,
    'project_id', @project_id_text::text);

-- name: PostflightIDSessionAttachments :one
SELECT count(*)
FROM principal_remote_session_bindings b
JOIN agents a ON a.id = b.principal_id AND a.organization_id = b.organization_id
JOIN remote_sessions rs ON rs.id = b.remote_session_id
JOIN user_sessions us ON us.id = @user_session_id::uuid
JOIN remote_session_client_user_session_issuers link
  ON link.remote_session_client_id = rs.remote_session_client_id
 AND link.user_session_issuer_id = us.user_session_issuer_id
WHERE b.organization_id = @org_id::text AND b.project_id = @project_id::uuid
  AND b.revoked_at IS NULL AND a.suspended_at IS NULL AND a.revoked_at IS NULL
  AND b.remote_session_id = @remote_session_id::uuid
  AND b.remote_session_client_id = rs.remote_session_client_id
  AND b.grant_generation = rs.grant_generation
  AND b.user_session_issuer_id = us.user_session_issuer_id
  AND rs.subject_urn = us.subject_urn
  AND rs.subject_urn = 'user:' || a.owner_user_id
  AND b.attached_by_subject_id = 'user:' || a.owner_user_id
  AND rs.access_token_encrypted = 'DEMO-NOT-VALID-CIPHERTEXT'
  AND rs.refresh_token_encrypted IS NULL AND rs.auto_refresh IS FALSE;

-- name: PostflightIDSessionBindings :one
SELECT count(*) FROM principal_remote_session_bindings
WHERE organization_id = @org_id::text;

-- name: PostflightIDUserOwnedUpstreamSessions :one
SELECT count(*) FROM remote_sessions
WHERE user_session_issuer_id IN
  (SELECT id FROM user_session_issuers WHERE project_id = @project_id::uuid);

-- name: PostflightIDInertUpstreamSessions :one
SELECT count(*) FROM remote_sessions
WHERE remote_session_client_id = @remote_session_client_id::uuid
  AND deleted IS FALSE
  AND access_token_encrypted = 'DEMO-NOT-VALID-CIPHERTEXT' AND refresh_token_encrypted IS NULL
  AND (validation_status IS NULL
       OR validation_status IN ('valid', 'rejected_by_member', 'inactive', 'unknown'));

-- name: PostflightIDAgents :one
SELECT count(*) FROM agents WHERE organization_id = @org_id::text;

-- name: PostflightIDAgentSessions :one
SELECT count(*) FROM user_sessions
WHERE organization_id = @org_id::text AND subject_urn LIKE 'agent:%';

-- This External OAuth row drives the metadata recommendation. Keep its count
-- stable so a rerun cannot duplicate it or silently drop the demo surface.
-- name: PostflightIDExternalOAuthMetadata :one
SELECT count(*) FROM external_oauth_server_metadata
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- name: PostflightIDChainingIssuer :one
SELECT count(*) FROM remote_session_issuers
WHERE project_id = @project_id::uuid AND organization_id = @org_id::text
  AND id = @chaining_issuer::uuid
  AND metadata IS NULL AND metadata_fetched_at IS NULL AND deleted IS FALSE;

-- name: PostflightIDChainingClient :one
SELECT count(*) FROM remote_session_clients
WHERE project_id = @project_id::uuid AND id = @chaining_client::uuid
  AND remote_session_issuer_id = @chaining_issuer::uuid
  AND grant_types = ARRAY['urn:ietf:params:oauth:grant-type:jwt-bearer']
  AND client_secret_encrypted IS NULL AND deleted IS FALSE;

-- name: PostflightIDJamfIssuer :one
SELECT count(*) FROM remote_session_issuers
WHERE project_id = @project_id::uuid AND organization_id = @org_id::text
  AND id = @jamf_issuer::uuid
  AND token_endpoint IS NOT NULL AND token_endpoint <> ''
  AND authorization_endpoint IS NULL AND jwks_uri IS NULL
  AND metadata IS NULL AND metadata_fetched_at IS NULL AND deleted IS FALSE;

-- Exactly one self client in the tenant.
-- name: PostflightIDSelfClients :one
SELECT count(*) FROM remote_session_clients
WHERE (project_id = @project_id::uuid OR organization_id = @org_id::text)
  AND credential_owner = 'self' AND deleted IS FALSE;

-- Its secret is the inert ciphertext.
-- name: PostflightIDJamfSelfClient :one
SELECT count(*) FROM remote_session_clients
WHERE project_id = @project_id::uuid
  AND id = @jamf_client::uuid
  AND remote_session_issuer_id = @jamf_issuer::uuid
  AND credential_owner = 'self'
  AND token_endpoint_auth_method = 'client_secret_post'
  AND client_secret_encrypted = 'DEMO-NOT-VALID-CIPHERTEXT'
  AND grant_types = ARRAY['client_credentials']
  AND deleted IS FALSE;

-- Nobody connects a self client, and no MCP server uses this one.
-- name: PostflightIDJamfClientIssuerBindings :one
SELECT count(*) FROM remote_session_client_user_session_issuers
WHERE remote_session_client_id = @jamf_client::uuid;

-- name: PostflightIDJamfClientSessions :one
SELECT count(*) FROM remote_sessions
WHERE remote_session_client_id = @jamf_client::uuid;

-- name: PostflightIDJamfIssuerServers :one
SELECT count(*) FROM mcp_servers
WHERE project_id = @project_id::uuid
  AND remote_session_issuer_id = @jamf_issuer::uuid;

-- The registrations are the point of the Connections surfaces: one per
-- credential kind, plus the pre-column row. A rerun that dropped or
-- duplicated any of them would leave the badges telling a different story
-- than the one they were seeded to tell.
-- Two upstream MCP issuers, two independent gateway issuers, one
-- Connections issuer, and the approvals server's issuer; GitHub uses the
-- organization workforce issuer.
-- name: PostflightIDProjectUserSessionIssuers :one
SELECT count(*) FROM user_session_issuers
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- name: PostflightIDOrgUserSessionIssuers :one
SELECT count(*) FROM user_session_issuers
WHERE organization_id = @org_id::text AND project_id IS NULL AND deleted IS FALSE;

-- The workforce issuer shows a shared authorization server.
-- name: PostflightIDWorkforceIssuer :one
SELECT count(*) FROM user_session_issuers
WHERE id = @workforce_issuer::uuid
  AND organization_id = @org_id::text
  AND authorization_server_mode = 'shared'
  AND deleted IS FALSE;

-- name: PostflightIDUserSessionClients :one
SELECT count(*) FROM user_session_clients
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- EMA: two ready bindings, no cached credentials, and one saved provider
-- order. The workforce sign-in must not trust the fictional provider.
-- name: PostflightIDWorkforceTrustsEMAProvider :one
SELECT count(*) FROM user_session_issuers
WHERE id = @workforce_issuer::uuid
  AND organization_id = @org_id::text
  AND trusted_remote_session_issuer_id = @ema_provider::uuid;

-- name: PostflightIDReadyEMABindings :one
SELECT count(*) FROM remote_session_ema_bindings
WHERE organization_id = @org_id::text AND project_id = @project_id::uuid
  AND state = 'ready';

-- name: PostflightIDEMACredentials :one
SELECT count(*) FROM remote_session_ema_credentials
WHERE organization_id = @org_id::text;

-- name: PostflightIDTrustedIssuerSessions :one
SELECT count(*) FROM trusted_issuer_sessions
WHERE organization_id = @org_id::text;

-- name: PostflightIDSavedEMAProviders :one
SELECT count(*) FROM remote_session_ema_providers
WHERE organization_id = @org_id::text AND deleted IS FALSE;

-- Linear's remote identity provider, the identity chaining example, Jamf
-- Pro, and the two EMA services' authorization servers.
-- name: PostflightIDProjectRemoteSessionIssuers :one
SELECT count(*) FROM remote_session_issuers
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- name: PostflightIDRemoteIdentityClients :one
SELECT count(*) FROM remote_session_clients
WHERE project_id = @project_id::uuid AND organization_id = @org_id::text
  AND client_id_issued_at IS NOT NULL AND deleted IS FALSE;

-- name: PostflightIDRemoteIdentityBindings :one
SELECT count(*)
FROM remote_session_client_user_session_issuers link
JOIN user_session_issuers usi ON usi.id = link.user_session_issuer_id
WHERE usi.project_id = @project_id::uuid;

-- The stamp has to be what ResyncMCPServerRemoteSessionIssuers would derive:
-- every live client bound to Linear's user session issuer names one remote
-- issuer, and it is the stamped one. Returns no row (pgx.ErrNoRows) when the
-- HAVING fails, including when no live client is bound at all.
-- name: PostflightIDLinearIssuerStamp :one
SELECT count(*)
FROM remote_session_client_user_session_issuers link
JOIN remote_session_clients c ON c.id = link.remote_session_client_id
WHERE link.user_session_issuer_id = @linear_issuer::uuid
  AND c.deleted IS FALSE
HAVING bool_and(c.remote_session_issuer_id = @linear_remote_issuer::uuid);

-- name: PostflightIDLinearStampedServers :one
SELECT count(*) FROM mcp_servers
WHERE project_id = @project_id::uuid
  AND remote_session_issuer_id = @linear_remote_issuer::uuid;

-- name: PostflightIDLinearProtectedResource :one
SELECT count(*) FROM remote_protected_resources
WHERE project_id = @project_id::uuid AND deleted IS FALSE
  AND resource_identifier = 'https://mcp.linear.app/mcp'
  AND scope_override = ARRAY['read'];

-- name: PostflightIDServerClientAssociations :one
SELECT count(*) FROM mcp_server_remote_session_clients
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- Check both the total and the expected credential/server/client tuples so
-- eight misassigned choices cannot satisfy postflight.
-- name: PostflightIDUpstreamClientChoices :one
WITH live AS (
  SELECT * FROM upstream_client_selections
  WHERE project_id = @project_id::uuid AND deleted IS FALSE
)
SELECT count(*) FROM live
WHERE (SELECT count(*) FROM live) = 8
  AND credential_kind = 'user_session'
  AND (
    (credential_id = ANY(@linear_session_ids::uuid[])
     AND mcp_server_id = @linear_server_id::uuid
     AND remote_session_client_id = @linear_client_id::uuid
     AND no_auth IS FALSE)
    OR
    (credential_id = ANY(@slack_session_ids::uuid[])
     AND mcp_server_id = @slack_server_id::uuid
     AND remote_session_client_id IS NULL
     AND client_attachment_scope IS NULL
     AND no_auth IS TRUE)
  );

-- Live choices that are not the intended ones: Linear through its main
-- client, Slack with no upstream authentication, each on a seeded session.
-- name: PostflightIDMisassignedUpstreamClientChoices :one
SELECT count(*)
FROM upstream_client_selections u
WHERE u.project_id = @project_id::uuid AND u.deleted IS FALSE
  AND NOT (
    EXISTS (SELECT 1 FROM user_sessions s WHERE s.id = u.credential_id AND s.deleted IS FALSE)
    AND (
      (u.mcp_server_id = @linear_server_id::uuid AND u.remote_session_client_id = @linear_client_id::uuid)
      OR (u.mcp_server_id = @slack_server_id::uuid AND u.no_auth)
    )
  );

-- name: PostflightIDServiceAccountHeaders :one
SELECT count(*)
FROM remote_mcp_server_headers header
JOIN remote_mcp_servers remote ON remote.id = header.remote_mcp_server_id
WHERE remote.project_id = @project_id::uuid AND header.deleted IS FALSE
  AND lower(header.name) = 'authorization' AND header.value IS NOT NULL;

-- Managed-agent credentials are a separate surface from ordinary MCP
-- connections, even though both fixtures belong to the same project.
-- name: PostflightIDProjectAgentSessions :one
SELECT count(*) FROM user_sessions
WHERE project_id = @project_id::uuid AND deleted IS FALSE
  AND subject_urn LIKE 'agent:%';

-- name: PostflightIDConnections :one
SELECT count(*) FROM user_sessions
WHERE project_id = @project_id::uuid AND deleted IS FALSE
  AND subject_urn NOT LIKE 'agent:%'
  AND subject_urn NOT LIKE 'workload:%';

-- Spread across servers, not pooled on one: the connections tab groups by
-- MCP server, and a single group makes that view look broken.
-- name: PostflightIDConnectionServers :one
SELECT count(DISTINCT user_session_issuer_id) FROM user_sessions
WHERE project_id = @project_id::uuid AND deleted IS FALSE
  AND subject_urn NOT LIKE 'agent:%'
  AND subject_urn NOT LIKE 'workload:%';

-- Everyone holds one. A person whose usage panels show a week of traffic
-- beside an empty connections tab reads as a broken join.
-- name: PostflightIDUsersWithoutConnection :one
SELECT count(*) FROM unnest(@user_ids::text[]) AS u(id)
WHERE NOT EXISTS (
  SELECT 1 FROM user_sessions s
  WHERE s.project_id = @project_id::uuid AND s.deleted IS FALSE
    AND s.subject_urn = 'user:' || u.id);

-- Every seeded connection hangs off a registration. One with none would be
-- filed under "Unknown client" and carry no credential reading at all.
-- name: PostflightIDUnregisteredConnections :one
SELECT count(*) FROM user_sessions
WHERE project_id = @project_id::uuid AND deleted IS FALSE AND user_session_client_id IS NULL
  AND subject_urn NOT LIKE 'agent:%'
  AND subject_urn NOT LIKE 'workload:%';

-- The legacy assistant stays without a dedicated agent.
-- name: PostflightIDAssistants :one
SELECT count(*) FROM assistants
WHERE organization_id = @org_id::text AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: PostflightIDTriggerInstances :one
SELECT count(*) FROM trigger_instances
WHERE organization_id = @org_id::text AND project_id = @project_id::uuid AND deleted IS FALSE;

-- name: PostflightIDAssistantAgentBindings :one
SELECT count(*) FROM assistant_agent_bindings
WHERE organization_id = @org_id::text AND project_id = @project_id::uuid;

-- name: PostflightIDAssistantThreads :one
SELECT count(*) FROM assistant_threads WHERE project_id = @project_id::uuid;

-- name: PostflightIDTriggerWorkloadBindings :one
SELECT count(*) FROM trigger_workload_bindings
WHERE organization_id = @org_id::text AND project_id = @project_id::uuid;

-- name: PostflightIDBoundAssistantRoot :one
SELECT count(*)
FROM trigger_workload_bindings t
JOIN assistant_agent_bindings b ON b.id = t.assistant_binding_id
JOIN assistants a ON a.id = b.assistant_id
JOIN agents ag ON ag.id = b.agent_id
JOIN trigger_instances r ON r.id = t.trigger_id
JOIN workload_issuers i ON i.id = t.workload_issuer_id
JOIN workload_identity_admissions adm ON adm.workload_issuer_id = i.id
  AND adm.organization_id = @org_id::text AND adm.project_id = @project_id::uuid
  AND adm.subject = t.subject AND adm.match_kind = 'exact' AND adm.deleted IS FALSE
JOIN workload_agent_assignments wa ON wa.workload_issuer_id = i.id
  AND wa.organization_id = @org_id::text AND wa.agent_id = ag.id
  AND wa.subject = t.subject AND wa.match_kind = 'exact' AND wa.deleted IS FALSE
WHERE t.organization_id = @org_id::text AND t.project_id = @project_id::uuid
  AND t.deleted IS FALSE AND b.deleted IS FALSE
  AND a.id = @assistant_id::uuid
  AND ag.project_id = @project_id::uuid AND ag.owner_user_id = @owner_user_id::text
  AND ag.suspended_at IS NULL AND ag.revoked_at IS NULL
  AND i.issuer = 'https://platform.example.invalid' AND i.jwks_uri = 'https://platform.example.invalid/.well-known/jwks.json' AND i.allow_wildcard_admission IS FALSE
  AND r.definition_slug = 'dashboard' AND r.target_kind = 'assistant' AND r.target_ref = a.id::text
  AND t.subject = 'assistant-trigger:' || r.id::text
  AND (SELECT count(*) FROM principal_grants g
    WHERE g.organization_id = @org_id::text AND g.principal_urn = 'agent:' || ag.id::text) = 4;

-- Workload sessions, and the trust configuration that labels them. A
-- workload whose issuer or assignment went missing would render as an
-- unnamed issuer with no agent, which is a real state but not the one seeded.
-- name: PostflightIDWorkloadIssuers :one
SELECT count(*) FROM workload_issuers
WHERE organization_id = @org_id::text AND deleted IS FALSE;

-- name: PostflightIDWorkloadAdmissions :one
SELECT count(*) FROM workload_identity_admissions
WHERE organization_id = @org_id::text AND deleted IS FALSE;

-- The seed writes these tables in raw SQL, so it never passes through
-- workloadidentity.ValidateSubjectRule the way the management API does. This
-- assert is that validation, applied to what was actually written, over both
-- tables that carry a subject rule: an admission decides whether a machine is
-- recognised at all, an assignment decides what it inherits, and the two have
-- to widen together. Anything wrong here reseeds daily into the demo
-- organization and reads as working configuration.
-- name: PostflightIDInvalidSubjectRules :one
WITH rules AS (
  SELECT 'admission' AS source, a.subject, a.match_kind, i.allow_wildcard_admission
  FROM workload_identity_admissions a
  JOIN workload_issuers i ON i.id = a.workload_issuer_id
  WHERE a.organization_id = @org_id::text AND a.deleted IS FALSE
  UNION ALL
  SELECT 'assignment', g.subject, g.match_kind, i.allow_wildcard_admission
  FROM workload_agent_assignments g
  JOIN workload_issuers i ON i.id = g.workload_issuer_id
  WHERE g.organization_id = @org_id::text AND g.deleted IS FALSE
)
SELECT count(*) AS stray,
  coalesce(string_agg(format('%s %L', source, subject), ', '), '')::text AS stray_detail
FROM rules
WHERE CASE match_kind
  -- A subject is otherwise opaque, so no character would be reserved. A "*"
  -- is refused anyway: no platform puts one in a sub, so its presence means
  -- the author wanted a wildcard and instead stored a literal matching
  -- nothing at all.
  WHEN 'exact' THEN subject LIKE '%*%'
  WHEN 'wildcard' THEN
    -- Enforced on read, so clearing the issuer flag is a kill switch. A rule
    -- written under an issuer that forbids wildcards is already inert.
    allow_wildcard_admission IS FALSE
    -- The terminator is mandatory and is the only "*" permitted: everything
    -- before it is the stem, which has to be a non-empty prefix rather than
    -- "match anything".
    OR subject NOT LIKE '%*'
    OR left(subject, length(subject) - 1) = ''
    OR left(subject, length(subject) - 1) LIKE '%*%'
  ELSE TRUE
END;

-- name: PostflightIDWorkloadAssignments :one
SELECT count(*) FROM workload_agent_assignments
WHERE organization_id = @org_id::text AND deleted IS FALSE;

-- An admission may not out-reach its issuer's tier: an organization-tier
-- admission has to name an organization-tier issuer. The management API refuses
-- the pairing and the admission lookup ignores it, so a seeded row in that shape
-- would be silently inert — the page would list it and no workload would match.
-- name: PostflightIDAdmissionsOutreachingIssuer :one
SELECT count(*)
FROM workload_identity_admissions a
JOIN workload_issuers i ON i.organization_id = a.organization_id AND i.id = a.workload_issuer_id
WHERE a.organization_id = @org_id::text AND a.deleted IS FALSE AND i.deleted IS FALSE
  AND i.project_id IS NOT NULL
  AND (a.project_id IS NULL OR a.project_id <> i.project_id);

-- A wildcard rule standing for a fleet is the headline of this fixture, so its
-- agent has to be one a workload can actually inherit. A suspended or revoked
-- agent resolves and is then denied, which reads as a broken feature.
-- name: PostflightIDWildcardAssignmentsToInactiveAgents :one
SELECT count(*)
FROM workload_agent_assignments g
JOIN agents ag ON ag.organization_id = g.organization_id AND ag.id = g.agent_id
WHERE g.organization_id = @org_id::text AND g.deleted IS FALSE
  AND g.match_kind = 'wildcard'
  AND (ag.suspended_at IS NOT NULL OR ag.revoked_at IS NOT NULL);

-- name: PostflightIDWorkloadSessions :one
SELECT count(*) FROM user_sessions
WHERE project_id = @project_id::uuid AND deleted IS FALSE
  AND subject_urn LIKE 'workload:%';

-- The Shadow AI status column is only worth looking at when it shows more
-- than one answer. Only a decided tool gets a row here — unreviewed IS the
-- absence of one — so the table stores just the two decided states, and
-- both have to survive the reseed with their full complement of rows.
-- name: PostflightIDApprovedAITools :one
SELECT count(*) FROM ai_scan_targets
WHERE organization_id = @org_id::text AND status = 'approved';

-- name: PostflightIDBlockedAITools :one
SELECT count(*) FROM ai_scan_targets
WHERE organization_id = @org_id::text AND status = 'blocked';

-- Anything else in the column would be a row the seed no longer means to
-- write: an undecided overlay row renders as unreviewed and is
-- indistinguishable on the page from a tool nobody has touched.
-- name: PostflightIDUndecidedAITools :one
SELECT count(*) FROM ai_scan_targets
WHERE organization_id = @org_id::text AND status NOT IN ('approved', 'blocked');

-- name: PostflightIDDirectoryRoleMappings :one
SELECT count(*) FROM directory_role_mappings
WHERE organization_id = @org_id::text AND deleted IS FALSE;

-- name: PostflightIDPendingDirectoryReviewItems :one
SELECT count(*) FROM directory_review_items
WHERE organization_id = @org_id::text AND state = 'pending';

-- name: PostflightIDDirectoryReviewEmailsEnabled :one
SELECT count(*) FROM directory_review_org_states
WHERE organization_id = @org_id::text AND emails_disabled_at IS NULL;

-- name: PostflightIDDeletedGroupMappings :one
SELECT count(*) FROM directory_role_mappings drm
JOIN directory_groups dg ON dg.id = drm.directory_group_id
  AND dg.organization_id = drm.organization_id
WHERE drm.organization_id = @org_id::text AND drm.deleted IS FALSE
  AND dg.workos_deleted IS TRUE;

-- name: PostflightIDInfraGroupMappedRoles :one
SELECT count(DISTINCT drm.role_urn) FROM directory_role_mappings drm
JOIN directory_groups dg ON dg.id = drm.directory_group_id
  AND dg.organization_id = drm.organization_id
WHERE drm.organization_id = @org_id::text AND drm.deleted IS FALSE
  AND dg.name = 'Infra'
  AND dg.deleted IS FALSE AND dg.workos_deleted IS FALSE;

-- name: PostflightIDGitHubCheckAccessGrants :one
SELECT count(*) FROM principal_grants
WHERE organization_id = @org_id::text
  AND id IN (@contractors_block::uuid, @direct_grant::uuid, @wildcard_grant::uuid);

-- name: PostflightIDSlackIdentityMappings :one
SELECT count(*) FROM slack_identity_mappings
WHERE organization_id = @org_id::text AND revoked_at IS NULL;

-- name: PostflightIDMessageParticipants :one
SELECT count(*) FROM chat_message_participants WHERE project_id = @project_id::uuid;

-- name: PostflightIDSlackDirectoryMemberships :one
SELECT count(*) FROM slack_directory_memberships WHERE organization_id = @org_id::text;

-- name: PostflightIDSlackDirectoryConnections :one
SELECT count(*) FROM slack_directory_connections WHERE organization_id = @org_id::text;

-- name: PostflightIDWidgets :one
SELECT count(*) FROM widgets WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- Sigint postflights preserve the complete authoring story: all modes,
-- ordered memberships, a shared catalog entry, and an incomplete draft.
-- name: PostflightIDSigintSignals :one
SELECT count(*) FROM sigint_custom_signals
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- name: PostflightIDSigintSensors :one
SELECT count(*) FROM sigint_sensors
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- name: PostflightIDSigintSensorModes :one
SELECT count(*) FROM sigint_sensors
WHERE project_id = @project_id::uuid AND deleted IS FALSE
  AND (id, mode) IN (
    (@service_areas::uuid, 'multi_label'),
    (@primary_service::uuid, 'exclusive'),
    (@impact::uuid, 'ordered_score'),
    (@draft_campaign::uuid, 'multi_label')
  );

-- name: PostflightIDSigintMemberships :one
SELECT count(*) FROM sigint_sensor_signals
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- name: PostflightIDSigintSignalMemberships :one
SELECT count(*)
FROM sigint_sensor_signals
WHERE project_id = @project_id::uuid AND deleted IS FALSE
  AND signal_id = @signal_id::uuid;

-- name: PostflightIDSigintSensorMemberships :one
SELECT count(*)
FROM sigint_sensor_signals
WHERE project_id = @project_id::uuid AND deleted IS FALSE
  AND sensor_id = @sensor_id::uuid;

-- name: PostflightIDSigintOrderedScore :one
SELECT count(*) FROM (
  SELECT array_agg(signal_id ORDER BY sort_order) AS signal_ids
  FROM sigint_sensor_signals
  WHERE project_id = @project_id::uuid AND deleted IS FALSE
    AND sensor_id = @impact::uuid
  HAVING array_agg(signal_id ORDER BY sort_order) = ARRAY[
    @signal_low::uuid,
    @signal_medium::uuid,
    @signal_high::uuid
  ]::uuid[]
) ordered_score;

-- name: PostflightIDSigintNonConsecutiveOrder :one
SELECT count(*) FROM (
  SELECT sort_order,
         row_number() OVER (
           PARTITION BY sensor_id ORDER BY sort_order, id
         ) - 1 AS expected_order
  FROM sigint_sensor_signals
  WHERE project_id = @project_id::uuid AND deleted IS FALSE
) memberships
WHERE sort_order <> expected_order;

-- name: PostflightIDSigintDanglingMemberships :one
SELECT count(*)
FROM sigint_sensor_signals ss
LEFT JOIN sigint_sensors s
  ON s.project_id = ss.project_id AND s.id = ss.sensor_id
LEFT JOIN sigint_custom_signals cs
  ON cs.project_id = ss.project_id AND cs.id = ss.signal_id
WHERE ss.project_id = @project_id::uuid AND ss.deleted IS FALSE
  AND (s.id IS NULL OR s.deleted IS TRUE
    OR cs.id IS NULL OR cs.deleted IS TRUE);

-- name: PostflightIDGatewayIssuers :one
SELECT count(DISTINCT user_session_issuer_id)::bigint
FROM meta_mcp_servers
WHERE project_id = @project_id::uuid AND deleted IS FALSE;

-- Only the dedicated workload and its owner may receive inference grants.
-- name: PostflightIDInferenceGrants :one
SELECT count(*) FROM principal_grants
WHERE organization_id = @org_id::text AND scope = 'inference:execute';

-- Verify admission, assignment, active agent and both explicit grant ceilings
-- together, rather than accepting unrelated rows that happen to meet counts.
-- name: PostflightIDInferenceWorkload :one
SELECT count(*)
FROM workload_issuers i
JOIN workload_identity_admissions a
  ON a.organization_id = i.organization_id AND a.workload_issuer_id = i.id
JOIN workload_agent_assignments w
  ON w.organization_id = a.organization_id AND w.workload_issuer_id = a.workload_issuer_id
  AND w.subject = a.subject AND w.match_kind = a.match_kind
JOIN agents ag ON ag.organization_id = w.organization_id AND ag.id = w.agent_id
JOIN principal_grants agent_grant
  ON agent_grant.organization_id = ag.organization_id AND agent_grant.principal_urn = 'agent:' || ag.id::text
JOIN principal_grants owner_grant
  ON owner_grant.organization_id = ag.organization_id AND owner_grant.principal_urn = 'user:' || ag.owner_user_id
WHERE i.organization_id = @org_id::text AND i.id = @issuer_id::uuid
  AND i.deleted IS FALSE AND i.project_id IS NULL
  AND i.issuer = 'https://inference.example.com'
  AND i.jwks_uri = 'https://inference.example.com/.well-known/jwks.json'
  AND i.allow_wildcard_admission IS FALSE
  AND a.deleted IS FALSE AND a.project_id = @project_id::uuid
  AND a.subject = 'service:acme:summary-worker' AND a.match_kind = 'exact'
  AND w.deleted IS FALSE AND ag.id = @agent_id::uuid
  AND ag.project_id = @project_id::uuid AND ag.suspended_at IS NULL AND ag.revoked_at IS NULL
  AND agent_grant.scope = 'inference:execute' AND owner_grant.scope = agent_grant.scope
  AND agent_grant.selectors = jsonb_build_object(
    'resource_kind', 'inference', 'resource_id', '*', 'project_id', ag.project_id::text)
  AND owner_grant.selectors = agent_grant.selectors;

-- Counts all bootstrap tokens in the organization, including soft-deleted
-- revoked rows. This asserts the total, not the distribution of statuses.
-- name: PostflightIDDeviceJoinTokens :one
SELECT count(*) FROM workload_device_join_tokens
WHERE organization_id = @org_id::text;
