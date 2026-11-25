-- Create "tool_calls" table
CREATE TABLE `gram`.`tool_calls` (
  `organization_id` String COMMENT 'Organization that owns the call.
@access: confidential' CODEC(ZSTD(1)),
  `project_id` String COMMENT 'Project that owns the call.
@access: confidential' CODEC(ZSTD(1)),
  `event_id` String COMMENT 'The tool call id that every observation of the call (decision, call, result) shares.
@access: confidential' CODEC(ZSTD(1)),
  `tool_name` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Tool the call ran.
@access: opaque-confidential',
  `mcp_server_name` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'MCP server the call went to, as reported.
@access: opaque-confidential',
  `mcp_tool_name` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'MCP tool the call invoked.
@access: opaque-confidential',
  `skill_name` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Skill the call invoked, empty for a plain tool call.
@access: opaque-confidential',
  `session_id` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Agent session the call belongs to.
@access: confidential',
  `user_email` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Email of the user the call ran for.
@access: confidential-pii',
  `surface` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'Agent surface that made the call.
@access: confidential',
  `status` AggregateFunction(argMaxIf, String, Int64, UInt8) COMMENT 'How the call ended: ok, error, rejected or refused.
@access: confidential',
  `duration_nano` AggregateFunction(argMaxIf, Int64, Int64, UInt8) COMMENT 'How long the call took, in nanoseconds.
@access: confidential',
  `started_at_unix_nano` SimpleAggregateFunction(min, Int64) COMMENT 'Earliest observation of the call.
@access: confidential',
  `ended_at_unix_nano` SimpleAggregateFunction(max, Int64) COMMENT 'Latest observation of the call.
@access: confidential',
  INDEX `idx_tool_calls_started_at` ((started_at_unix_nano)) TYPE minmax
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `event_id`) ORDER BY (`organization_id`, `project_id`, `event_id`) TTL fromUnixTimestamp64Nano(started_at_unix_nano) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'One row per tool call, folded from agent_events by tool_calls_mv as records arrive. Read with the -Merge combinators and GROUP BY organization_id, project_id, event_id. Filled forward only, from the time the view was created.';
-- Create "tool_calls_mv" view
CREATE MATERIALIZED VIEW `gram`.`tool_calls_mv` TO `gram`.`tool_calls` AS SELECT organization_id, project_id, event_id, argMaxIfState(tool_name, observed_at_unix_nano, tool_name != '') AS tool_name, argMaxIfState(mcp_server_name, observed_at_unix_nano, mcp_server_name != '') AS mcp_server_name, argMaxIfState(mcp_tool_name, observed_at_unix_nano, mcp_tool_name != '') AS mcp_tool_name, argMaxIfState(skill_name, observed_at_unix_nano, skill_name != '') AS skill_name, argMaxIfState(session_id, observed_at_unix_nano, session_id != '') AS session_id, argMaxIfState(user_email, observed_at_unix_nano, user_email != '') AS user_email, argMaxIfState(toString(surface), observed_at_unix_nano, surface != '') AS surface, argMaxIfState(toString(outcome), observed_at_unix_nano, outcome != '') AS status, argMaxIfState(duration_nano, observed_at_unix_nano, duration_nano != 0) AS duration_nano, min(occurred_at_unix_nano) AS started_at_unix_nano, max(occurred_at_unix_nano) AS ended_at_unix_nano FROM gram.agent_events WHERE (event_type IN ('tool_call', 'tool_call_result', 'tool_decision')) AND (event_id != '') GROUP BY organization_id, project_id, event_id;
