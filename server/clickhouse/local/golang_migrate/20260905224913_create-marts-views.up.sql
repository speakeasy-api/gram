-- create "weekly_ai_surface_adoption" view
CREATE VIEW `marts`.`weekly_ai_surface_adoption` (
  `week_start` Date,
  `surface` String,
  `active_users` UInt64
) DEFINER = `marts_definer` SQL SECURITY DEFINER AS SELECT toMonday(time_bucket) AS week_start, if((hook_source IN ('claude-code', 'codex', 'cowork', 'cursor', 'litellm', 'local', 'mcp', 'openclaw', 'opencode')), hook_source, 'other') AS surface, uniqExactIf(user_email, user_email != '') AS active_users FROM gram.attribute_metrics_summaries WHERE (is_active = 1) AND (time_bucket < toDateTime(toMonday(now('UTC')), 'UTC')) AND (hook_source NOT IN ('', 'assistants', 'chat-analysis', 'elements', 'gram', 'mcp-research', 'playground', 'risk-analysis', 'skill-efficacy', 'skill-suggestions', 'slack')) GROUP BY week_start, surface HAVING active_users >= 10;
