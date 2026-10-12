-- Backfill session_transcript from agent_events for one UTC day. The view only
-- folds rows inserted after it was created, so run this once per retained day,
-- oldest first, up to and including the day the view went live:
--
--   clickhouse-client --param_day=20260901 --queries-file session_transcript.sql
--
-- Every state the view writes is idempotent, so re-running a day is safe. The
-- SELECT is the view's own, from schema.sql, plus the day bound. Keep them
-- identical.
INSERT INTO session_transcript
WITH (observed_at_unix_nano, multiIf(agent_events.channel = 'gateway', 3, agent_events.channel = 'otel', 2, 1), record_id) AS observation_order
SELECT
    organization_id,
    project_id,
    session_id,
    event_id,
    min(occurred_at_unix_nano) AS started_at_unix_nano,
    max(occurred_at_unix_nano) AS ended_at_unix_nano,
    argMaxIfState(toString(event_type), observation_order, event_type != '') AS event_type,
    argMaxIfState(turn_id, observation_order, turn_id != '') AS turn_id,
    argMaxIfState(if(name != '', name, tool_name), observation_order, name != '' OR tool_name != '') AS name,
    argMaxIfState(text, observation_order, text != '') AS text,
    argMaxIfState(toString(outcome), observation_order, outcome != '') AS outcome,
    argMaxIfState(outcome_message, observation_order, outcome_message != '') AS outcome_message,
    argMaxIfState(model, observation_order, model != '') AS model,
    argMaxIfState(toString(provider), observation_order, provider != '') AS provider,
    argMaxIfState(toString(surface), observation_order, surface != '') AS surface,
    argMaxIfState(toString(source), observation_order, source != '') AS source,
    argMaxIfState(toString(channel), observation_order, channel != '') AS channel,
    argMaxIfState(user_id, observation_order, user_id != '') AS user_id,
    argMaxIfState(user_email, observation_order, user_email != '') AS user_email,
    argMaxIfState(external_user_id, observation_order, external_user_id != '') AS external_user_id,
    argMaxIfState(toString(attributes.gram.chat.id), observation_order, toString(attributes.gram.chat.id) != '') AS chat_id,
    argMaxIfState(duration_nano, observation_order, duration_nano != 0) AS duration_nano
FROM agent_events
WHERE session_id != '' AND event_id != ''
  AND ((agent_events.channel = 'chat' AND agent_events.event_type IN ('prompt', 'api_response'))
       OR agent_events.event_type IN ('tool_call', 'tool_call_result', 'tool_decision'))
  AND toYYYYMMDD(fromUnixTimestamp64Nano(occurred_at_unix_nano)) = {day:UInt32}
GROUP BY organization_id, project_id, session_id, event_id;
