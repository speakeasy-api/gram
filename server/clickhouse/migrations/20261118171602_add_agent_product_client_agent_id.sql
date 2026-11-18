ALTER TABLE `gram`.`agent_events`
  COMMENT COLUMN `surface` 'Agent surface as the producer named it (e.g. claude-code, codex, an MCP client name). Mixes product and client, so it is deprecated in favour of product and client, and written alongside them until a later contract migration drops it. Empty when not stated.
@access: confidential',
  ADD COLUMN `product` LowCardinality(String) COMMENT 'Agent product the activity happened in, folded to one vocabulary (e.g. claude-code, claude-chat, cowork, codex, cursor). Empty when the producer named none the fold recognizes.
@access: confidential' AFTER `surface`,
  ADD COLUMN `client` LowCardinality(String) COMMENT 'Kind of client the product ran in: cli | ide | desktop | web | mobile | cloud | slack. Empty when the producer gives no signal for it.
@access: confidential' AFTER `product`,
  ADD COLUMN `agent_id` String COMMENT 'Speakeasy agent principal the activity ran as, when an agent key or workload identity authenticated it. Set only from Speakeasy-emitted records, never from producer attributes. Empty for human activity.
@access: confidential' CODEC(ZSTD(1)) AFTER `external_user_id`;
ALTER TABLE `gram`.`tool_calls`
  COMMENT COLUMN `surface` 'Agent surface that made the call, as the producer named it. Deprecated in favour of product and client.
@access: confidential',
  ADD COLUMN `product` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Agent product that made the call.
@access: confidential' AFTER `surface`,
  ADD COLUMN `client` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Kind of client the product ran in.
@access: confidential' AFTER `product`,
  ADD COLUMN `agent_id` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Speakeasy agent principal the call ran as, empty for a human.
@access: confidential' AFTER `client`;
-- Hand-edited from Atlas generated DROP+recreate: swap the MV query atomically
-- so concurrent agent_events inserts keep reaching tool_calls.
ALTER TABLE `gram`.`tool_calls_mv` MODIFY QUERY SELECT organization_id, project_id, event_id, argMaxIfState(tool_name, observed_at_unix_nano, tool_name != '') AS tool_name, argMaxIfState(mcp_server_name, observed_at_unix_nano, mcp_server_name != '') AS mcp_server_name, argMaxIfState(mcp_tool_name, observed_at_unix_nano, mcp_tool_name != '') AS mcp_tool_name, argMaxIfState(skill_name, observed_at_unix_nano, skill_name != '') AS skill_name, argMaxIfState(session_id, observed_at_unix_nano, session_id != '') AS session_id, argMaxIfState(user_email, observed_at_unix_nano, user_email != '') AS user_email, argMaxIfState(toString(surface), observed_at_unix_nano, surface != '') AS surface, argMaxIfState(toString(product), observed_at_unix_nano, product != '') AS product, argMaxIfState(toString(client), observed_at_unix_nano, client != '') AS client, argMaxIfState(agent_id, observed_at_unix_nano, agent_id != '') AS agent_id, argMaxIfState(toString(outcome), observed_at_unix_nano, outcome != '') AS status, argMaxIfState(duration_nano, observed_at_unix_nano, duration_nano != 0) AS duration_nano, min(occurred_at_unix_nano) AS started_at_unix_nano, max(occurred_at_unix_nano) AS ended_at_unix_nano FROM gram.agent_events WHERE (event_type IN ('tool_call', 'tool_call_result', 'tool_decision')) AND (event_id != '') GROUP BY organization_id, project_id, event_id;
