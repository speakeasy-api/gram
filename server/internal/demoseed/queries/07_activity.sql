-- Activity phase: audit feed, AI integration and inference hooks, Claude Tag
-- and delivery chats, assistant sessions, trial and killswitch history, spend
-- rules, and Explore widgets. Every tenant identifier is a parameter.

-- name: ActivityDeleteAuditLogs :exec
DELETE FROM audit_logs WHERE organization_id = @organization_id;

-- name: ActivityInsertProjectAuditLog :exec
INSERT INTO audit_logs (id, organization_id, project_id, actor_id, actor_type,
                        actor_display_name, action, subject_id, subject_type,
                        subject_display_name, created_at)
VALUES (@id, @organization_id, @project_id::uuid,
        @actor_id, 'user', @actor_display_name::text,
        @action, @subject_id, 'toolset', @subject_display_name::text,
        now() - @age::interval);

-- name: ActivityDeleteAnthropicInferenceConfig :exec
DELETE FROM ai_integration_configs WHERE organization_id = @organization_id AND provider = 'anthropic_inference';

-- name: ActivityInsertAnthropicInferenceConfig :exec
INSERT INTO ai_integration_configs (id, organization_id, project_id, provider, api_key_encrypted, enabled)
VALUES (@id, @organization_id, @project_id, 'anthropic_inference', '', false);

-- agent_id names the Speakeasy agent principal that produced the session, as
-- hook capture stamps it. NULL for the human-driven sessions.
-- name: ActivityInsertChat :exec
INSERT INTO chats (id, project_id, organization_id, user_id, external_user_id, title, session_surface, agent_id, created_at, updated_at)
VALUES (@id, @project_id, @organization_id, @user_id::text, @external_user_id::text, @title::text,
        sqlc.narg(session_surface)::text, sqlc.narg(agent_id)::uuid,
        now() - @created_age::interval, now() - @updated_age::interval);

-- Columns a message does not set (content_raw, tool_calls, source, model) are
-- passed as NULL, which is what omitting them wrote.
-- name: ActivityInsertChatMessage :exec
INSERT INTO chat_messages (id, chat_id, project_id, role, content, content_raw, tool_calls, source, model, created_at, risk_analyzed_at)
VALUES (@id, @chat_id, @project_id::uuid, @role, @content,
        sqlc.narg(content_raw)::jsonb, sqlc.narg(tool_calls)::jsonb,
        sqlc.narg(source)::text, sqlc.narg(model)::text,
        now() - @age::interval, now());

-- name: ActivitySetClaudeTagChatSlack :exec
UPDATE chats SET session_surface = 'claude-tag', slack_team_id = 'T0DEMO0001', slack_channel_id = 'DEMO_CHANNEL', slack_channel_name = 'demo-releases'
WHERE id = @chat_id AND project_id = @project_id;

-- Each delivered turn names its Slack sender: owner-1 and the wake prompt came
-- from Slack member 1, owner-2 from member 3.
-- name: ActivityInsertClaudeTagParticipants :exec
INSERT INTO chat_message_participants (project_id, chat_id, message_id, provider, provider_user_id, provider_team_id, user_id, display_name)
SELECT @project_id::uuid, @chat_id::uuid, v.message_id, 'slack',
  m.slack_user_id, m.slack_team_id, im.user_id, m.display_name
FROM (VALUES (@owner1_message_id::uuid, @member1_id::uuid),
             (@owner2_message_id::uuid, @member3_id::uuid),
             (@prompt_message_id::uuid, @member1_id::uuid)) v(message_id, member_id)
JOIN slack_directory_memberships m ON m.organization_id = @organization_id::text AND m.id = v.member_id
LEFT JOIN slack_identity_mappings im ON im.organization_id = @organization_id::text AND im.slack_team_id = m.slack_team_id AND im.slack_user_id = m.slack_user_id AND im.revoked_at IS NULL;

-- name: ActivityInsertClaudeTagSessionLink :exec
INSERT INTO chat_session_links (project_id, organization_id, parent_chat_id, child_chat_id, parent_session_id, child_session_id, kind, target_harness, source_surface)
VALUES (@project_id, @organization_id, @parent_chat_id::uuid, @child_chat_id::uuid, (@parent_chat_id::uuid)::text, (@child_chat_id::uuid)::text, 'subagent', 'claude-tag', 'claude-tag');

-- name: ActivityInsertAssistantThread :exec
INSERT INTO assistant_threads (id, assistant_id, project_id, correlation_id, chat_id, source_kind, last_event_at)
VALUES (@id, @assistant_id, @project_id, @correlation_id, @chat_id, 'dashboard', now() - interval '38 minutes');

-- Historical shortened trial: audit history shows both dates, not an extension.
-- name: ActivityInsertTrialEndChangedAuditLog :exec
INSERT INTO audit_logs
  (id, organization_id, actor_id, actor_type, actor_display_name,
   action, subject_id, subject_type, before_snapshot, after_snapshot, metadata, created_at)
VALUES
  (@id, @organization_id::text,
   @actor_id, 'user', @actor_display_name::text,
   'organization:enterprise_trial_end_changed', @organization_id::text, 'organization',
   jsonb_build_object('trial_ends_at', now() - interval '1 hour'),
   jsonb_build_object('trial_ends_at', now() - interval '6 hours'),
   jsonb_build_object('previous_trial_ends_at', now() - interval '1 hour',
                     'trial_ends_at', now() - interval '6 hours'),
   now() - interval '12 hours');

-- Quarantine lifecycle events use their own audit subject and action rather
-- than reusing a generic policy-block row.
-- name: ActivityInsertQuarantineAuditLog :exec
INSERT INTO audit_logs (id, organization_id, project_id, actor_id, actor_type,
                        actor_display_name, action, subject_id, subject_type,
                        subject_display_name, subject_slug, metadata, created_at)
VALUES (@id, @organization_id,
        @project_id::uuid, @actor_id::text, 'user', @actor_display_name::text,
        'session_quarantine:open',
        @subject_id,
        'session_quarantine', 'Acme session quarantine policy',
        @session_id::text,
        jsonb_build_object(
          'session_id', @session_id::text,
          'risk_policy_id', @risk_policy_id::text,
          'risk_policy_name', 'Acme session quarantine policy',
          'user_id', @actor_id::text,
          'reason', 'Speakeasy quarantined this prompt after a demo policy match.'
        ),
        now() - interval '35 minutes');

-- Killswitch mutation rows carry a bounded after snapshot plus their replay
-- operation.
-- name: ActivityInsertKillswitchAuditLog :exec
INSERT INTO audit_logs
  (id, organization_id, project_id, actor_id, actor_type, actor_display_name,
   action, subject_id, subject_type, after_snapshot, metadata, created_at)
VALUES
  (@id, @organization_id, NULL,
   @actor_id, 'user', @actor_display_name::text, @action,
   @subject_id,
   'killswitch_prescription', jsonb_build_object('version', @version::int, 'state', @state::text),
   jsonb_build_object('operation', @operation::text, 'operation_id', @operation_id::uuid),
   now() - @age::interval);

-- Expiry carries the version and database-time deadline.
-- name: ActivityInsertKillswitchExpiredAuditLog :exec
INSERT INTO audit_logs
  (id, organization_id, project_id, actor_id, actor_type, actor_display_name,
   action, subject_id, subject_type, after_snapshot, metadata, created_at)
VALUES
  (@id, @organization_id, NULL,
   'system', 'user', 'System', 'killswitch:expire',
   @subject_id,
   'killswitch_prescription', NULL,
   jsonb_build_object('version', 1, 'expired_at', now() - interval '1 day'),
   now() - interval '23 hours');

-- name: ActivityDeleteSpendRuleEvents :exec
DELETE FROM spend_rule_events WHERE organization_id = @organization_id;

-- name: ActivityDeleteSpendRules :exec
DELETE FROM spend_rules WHERE organization_id = @organization_id;

-- name: ActivityInsertSpendRules :exec
INSERT INTO spend_rules (id, organization_id, name, slug, description, target_expr,
                         limit_usd_cents, window_kind, warn_at_pct, action, enabled,
                         version, created_at)
VALUES
  (@rule_monthly::uuid, @organization_id::text, 'Monthly agent budget', 'monthly-agent-budget',
   'Per-person monthly cap on agent/CLI spend.',
   @monthly_target_expr::text, 200000, 'monthly', 80, 'flag', TRUE, 1,
   now() - interval '20 days'),
  -- Builder-representable condition: the rule edit sheet cannot round-trip
  -- 'email in [...]' CEL (no is-one-of operator), which left the condition
  -- blank in the UI. Department equality maps cleanly.
  (@rule_weekly::uuid, @organization_id::text, 'Support weekly cap', 'support-weekly-cap',
   'Weekly guardrail for the support rotation.',
   'department_name == "Support Engineering"',
   60000, 'weekly', 75, 'block', TRUE, 1, now() - interval '15 days');

-- name: ActivityInsertSpendRuleEvents :exec
INSERT INTO spend_rule_events (organization_id, spend_rule_id, rule_urn, event_type,
                               user_id, email, display_name, spend_usd_cents,
                               limit_usd_cents, window_start, window_end, created_at)
VALUES
  (@organization_id::text, @rule_monthly::uuid, 'spend_rule:monthly-agent-budget:v1', 'breach',
   @mateo_id::text, @mateo_email::text, 'Mateo Alvarez', 231900, 200000,
   date_trunc('month', now()), date_trunc('month', now()) + interval '1 month',
   now() - interval '1 day'),
  (@organization_id::text, @rule_monthly::uuid, 'spend_rule:monthly-agent-budget:v1', 'warning',
   @lucas_id::text, @lucas_email::text, 'Lucas Meyer', 171400, 200000,
   date_trunc('month', now()), date_trunc('month', now()) + interval '1 month',
   now() - interval '2 days'),
  (@organization_id::text, @rule_weekly::uuid, 'spend_rule:support-weekly-cap:v1', 'breach',
   @jonas_id::text, @jonas_email::text, 'Jonas Lindqvist', 74200, 60000,
   date_trunc('week', now()), date_trunc('week', now()) + interval '1 week',
   now() - interval '6 hours'),
  (@organization_id::text, @rule_weekly::uuid, 'spend_rule:support-weekly-cap:v1', 'warning',
   @amara_id::text, @amara_email::text, 'Amara Okafor', 48700, 60000,
   date_trunc('week', now()), date_trunc('week', now()) + interval '1 week',
   now() - interval '10 hours');

-- Explore widgets: the questions the team keeps, each with the chart that
-- draws it and saved by a demo user, so the Widgets tab shows who made each.
-- Names repeat on purpose: two people saving "Tool calls by surface" is normal.
-- Each row is the shape the dashboard saves (widgetSpec.ts), and the widgets
-- service plans it against the analytics catalog and checks the chart against
-- it, so the list reads them as valid. Rows cascade with the demo project,
-- which the reset at the top deletes.
-- name: ActivityInsertWidgets :exec
INSERT INTO widgets (id, project_id, organization_id, created_by_user_id,
                     name, description, dataset, query, visualization,
                     created_at, updated_at)
VALUES
  (@widget1_id::uuid, @project_id::uuid, @organization_id::text, @amara_id::text,
   'Tool calls by surface', 'Which agents people reach for, day by day', 'tool_calls',
   '{"window":"7d","grain":"day","ungrouped":false,"dimensions":["surface"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[],"limit":1000}',
   '{"type":"bar","options":{}}',
   now() - interval '9 days', now() - interval '2 hours'),
  (@widget2_id::uuid, @project_id::uuid, @organization_id::text, @priya_id::text,
   'Slowest MCP tools', 'p95 latency per MCP server and tool, worst first', 'tool_calls',
   '{"window":"7d","grain":"none","ungrouped":false,"dimensions":["mcp_server","mcp_tool"],"measures":[{"op":"p95","field":"duration_ms","alias":"p95_duration_ms"},{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[{"measure":"p95_duration_ms","direction":"desc"}],"limit":20}',
   '{"type":"table","options":{}}',
   now() - interval '6 days', now() - interval '1 day'),
  (@widget3_id::uuid, @project_id::uuid, @organization_id::text, @jonas_id::text,
   'Tool calls by status', NULL, 'tool_calls',
   '{"window":"7d","grain":"day","ungrouped":false,"dimensions":["status"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[],"limit":1000}',
   '{"type":"line","options":{}}',
   now() - interval '5 days', now() - interval '3 days'),
  (@widget4_id::uuid, @project_id::uuid, @organization_id::text, @mateo_id::text,
   'Average call time by tool', NULL, 'tool_calls',
   '{"window":"30d","grain":"none","ungrouped":false,"dimensions":["tool_name"],"measures":[{"op":"avg","field":"duration_ms","alias":"avg_duration_ms"}],"filters":[],"order_by":[{"measure":"avg_duration_ms","direction":"desc"}],"limit":0}',
   '{"type":"ranked","options":{}}',
   now() - interval '8 days', now() - interval '4 days'),
  (@widget5_id::uuid, @project_id::uuid, @organization_id::text, @hana_id::text,
   'Tool calls by surface', NULL, 'tool_calls',
   '{"window":"30d","grain":"day","ungrouped":false,"dimensions":["surface"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[],"limit":1000}',
   '{"type":"area","options":{}}',
   now() - interval '11 days', now() - interval '6 days'),
  (@widget6_id::uuid, @project_id::uuid, @organization_id::text, @priya_id::text,
   'Calls by MCP server', 'Which servers the calls went to, day by day', 'tool_calls',
   '{"window":"7d","grain":"day","ungrouped":false,"dimensions":["mcp_server"],"measures":[{"op":"count","field":"","alias":"count"}],"filters":[],"order_by":[],"limit":1000}',
   '{"type":"stacked_bar","options":{}}',
   now() - interval '4 days', now() - interval '1 day');
