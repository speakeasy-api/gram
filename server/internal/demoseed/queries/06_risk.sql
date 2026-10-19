-- Risk policies + chats + transcripts + findings (postgres.sql lines
-- 2389-2826). The WHY behind each row lives next to the Go in
-- seed_06_risk.go.

-- name: RiskInsertPolicy :exec
-- `sources` values must be in validateSources (internal/risk/impl.go);
-- cli_destructive / destructive_tool / account_identity are flag-only
-- (validateSourceAction). A postflight assert checks the source names.
INSERT INTO risk_policies (id, project_id, organization_id, name, policy_type,
                           sources, presidio_entities, analyzer_config,
                           custom_rule_ids, mcp_scope,
                           enabled, action, audience_type,
                           shadow_mcp_disposition, auto_name, score, version)
VALUES (@id, @project_id, @organization_id, @name, 'standard',
        @sources::text[], sqlc.narg(presidio_entities)::text[], @analyzer_config::jsonb,
        @custom_rule_ids::text[], sqlc.narg(mcp_scope)::jsonb,
        @enabled, @action, 'everyone',
        sqlc.narg(shadow_mcp_disposition)::text, @auto_name, @score, 1);

-- name: RiskInsertBypassGrant :exec
-- The same canonical target has two grants, so Platform MCP demonstrates
-- target counts rather than leaking or counting the target audience.
INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors)
VALUES (@organization_id, @principal_urn::text, 'risk_policy:bypass',
        jsonb_build_object('resource_kind', 'risk_policy', 'resource_id', @resource_id::text,
                           'server_url', 'https://shadow-mcp.demo.getgram.ai/research'));

-- name: RiskInsertSessionQuarantine :exec
INSERT INTO session_quarantines
  (id, organization_id, project_id, session_id, risk_policy_id,
   risk_policy_name, user_id, reason, created_at, updated_at)
VALUES
  (@id, @organization_id, @project_id, @session_id, @risk_policy_id,
   'Acme session quarantine policy', @user_id,
   'Speakeasy quarantined this prompt after a demo policy match.',
   now() - interval '35 minutes', now() - interval '35 minutes');

-- name: RiskInsertCustomDetectionRules :exec
-- Custom CEL detection rules behind policy_cr; also the only data on the
-- Detection Rules page. detection_expr supersedes the legacy regex /
-- match_config columns, so both stay NULL. Escape-free matchers only:
-- matchRegex swallows an invalid pattern as "no match" (celenv.go), while
-- matchText is a case-insensitive literal substring. Compile-checked by
-- TestSeedCELCompiles.
INSERT INTO risk_custom_detection_rules
  (id, project_id, organization_id, rule_id, title, description,
   detection_expr, severity)
VALUES
  (@rule1_id::uuid, @project_id::uuid, @organization_id::text,
   'custom.sensitive_file_read', 'Credential file access',
   'Agent reads of SSH keys, cloud credentials, or dotenv files outside the project (OWASP LLM02).',
   'tool_calls.exists(t, t.args.get("file_path").matchText("/.ssh/") || t.args.get("file_path").matchText("/.aws/credentials") || t.args.get("command").matchText("/.ssh/id_") || t.args.get("command").matchText("/.aws/credentials"))',
   'high'),
  (@rule2_id::uuid, @project_id::uuid, @organization_id::text,
   'custom.env_secret_dump', 'Environment secret dump',
   'Agent dumping the process environment, where CI/CD tokens and API keys live (OWASP LLM02).',
   'tool_calls.exists(t, t.args.get("command").matchText("printenv") || t.args.get("command").matchText("/proc/self/environ") || t.args.get("command").matchText("env | curl"))',
   'critical'),
  (@rule3_id::uuid, @project_id::uuid, @organization_id::text,
   'custom.ssrf_metadata_endpoint', 'SSRF to internal endpoint',
   'Agent-controlled requests to cloud metadata or loopback addresses (MCP security best practices).',
   'tool_calls.exists(t, t.args.get("url").matchText("169.254.169.254") || t.args.get("url").matchText("http://localhost") || t.args.get("command").matchText("169.254.169.254"))',
   'critical');

-- name: RiskInsertExclusions :exec
-- Going-forward exclusions: the tuning a team accumulates once a policy has
-- been live for a while. Each of the first two suppresses exactly the
-- fixture value its rule's match rotation emits (see the finding type table
-- in seed_06_risk.go), so the suppressed findings the seed writes are
-- truthfully attributable to these rows rather than decorative. The third
-- has matched nothing yet — an exclusion with no suppressions is a normal
-- state and the one the empty-count rendering needs.
INSERT INTO risk_exclusions (id, project_id, organization_id, risk_policy_id,
                             match_type, match_value, rule_id_filter,
                             source_filter, enabled, created_at, updated_at)
VALUES
  (@excl_fixture_id::uuid, @project_id::uuid, @organization_id::text, @policy_cd_id::uuid,
   'exact', 'qa.fixture@example.com', 'pii.email_address', 'presidio',
   TRUE, now() - interval '9 days', now() - interval '9 days'),
  (@excl_testcard_id::uuid, @project_id::uuid, @organization_id::text, @policy_a_id::uuid,
   'exact', '4111 1111 1111 1111', 'pii.credit_card', 'presidio',
   TRUE, now() - interval '6 days', now() - interval '6 days'),
  (@excl_examplekey_id::uuid, @project_id::uuid, @organization_id::text, NULL,
   'regex', 'AKIAEXAMPLE[0-9A-Z]+', 'aws-access-token', 'gitleaks',
   TRUE, now() - interval '2 days', now() - interval '2 days');

-- name: RiskCopyChats :copyfrom
INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id,
                   title, created_at, updated_at)
VALUES (@id, @project_id, @organization_id, @user_id, @external_user_id,
        @title, @created_at, @updated_at);

-- name: RiskCopyChatMessages :copyfrom
-- One COPY for both message shapes: tool rows leave message_id NULL and the
-- token columns at their 0 default, the others leave tool_call_id NULL.
-- Rows arrive in the old loop's order, so chat_messages.seq is assigned
-- identically.
INSERT INTO chat_messages (id, chat_id, project_id, role, content, model,
                           message_id, tool_call_id, source,
                           prompt_tokens, completion_tokens, total_tokens,
                           created_at, risk_analyzed_at)
VALUES (@id, @chat_id, @project_id, @role, @content, @model,
        @message_id, @tool_call_id, @source,
        @prompt_tokens, @completion_tokens, @total_tokens,
        @created_at, @risk_analyzed_at);

-- name: RiskCopyRiskResults :copyfrom
INSERT INTO risk_results (id, project_id, organization_id, risk_policy_id,
                          risk_policy_version, chat_message_id, source, found,
                          rule_id, description, match, start_pos, end_pos,
                          confidence, tags, created_at,
                          excluded_at, excluded_exclusion_id,
                          false_positive_at, false_positive_reason)
VALUES (@id, @project_id, @organization_id, @risk_policy_id,
        @risk_policy_version, @chat_message_id, @source, @found,
        @rule_id, @description, @match, @start_pos, @end_pos,
        @confidence, @tags, @created_at,
        @excluded_at, @excluded_exclusion_id,
        @false_positive_at, @false_positive_reason);
