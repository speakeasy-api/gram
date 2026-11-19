-- Reverse the MV query atomically before removing the columns it reads.
ALTER TABLE `gram`.`tool_calls_mv` MODIFY QUERY SELECT organization_id, project_id, event_id, argMaxIfState(tool_name, observed_at_unix_nano, tool_name != '') AS tool_name, argMaxIfState(mcp_server_name, observed_at_unix_nano, mcp_server_name != '') AS mcp_server_name, argMaxIfState(mcp_tool_name, observed_at_unix_nano, mcp_tool_name != '') AS mcp_tool_name, argMaxIfState(skill_name, observed_at_unix_nano, skill_name != '') AS skill_name, argMaxIfState(session_id, observed_at_unix_nano, session_id != '') AS session_id, argMaxIfState(user_email, observed_at_unix_nano, user_email != '') AS user_email, argMaxIfState(toString(surface), observed_at_unix_nano, surface != '') AS surface, argMaxIfState(toString(outcome), observed_at_unix_nano, outcome != '') AS status, argMaxIfState(duration_nano, observed_at_unix_nano, duration_nano != 0) AS duration_nano, min(occurred_at_unix_nano) AS started_at_unix_nano, max(occurred_at_unix_nano) AS ended_at_unix_nano FROM gram.agent_events WHERE (event_type IN ('tool_call', 'tool_call_result', 'tool_decision')) AND (event_id != '') GROUP BY organization_id, project_id, event_id;
ALTER TABLE `gram`.`tool_calls`
  DROP COLUMN `agent_id`,
  DROP COLUMN `client`,
  DROP COLUMN `product`,
  COMMENT COLUMN `surface` 'Agent surface that made the call.
@access: confidential';
ALTER TABLE `gram`.`agent_events`
  DROP COLUMN `agent_id`,
  DROP COLUMN `client`,
  DROP COLUMN `product`,
  COMMENT COLUMN `surface` 'Agent surface the activity happened on (e.g. claude-code, codex, cursor). Empty when not stated.
@access: confidential';
