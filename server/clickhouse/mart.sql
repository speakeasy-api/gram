-- The marts database is an allowlist boundary for employee and agent analytics.
-- Keep it view-only. Never expose raw identifiers, free-form customer content,
-- customer-defined names, or monetary values here. Every published row must
-- represent at least ten distinct identified users.
-- The database, access principals, and reader limits are provisioned
-- outside schema migrations: Terraform in Cloud, local/clickhouse/initdb locally.
-- Atlas uses the same bootstrap as its development database baseline.
-- Atlas ignores grants in desired state; keep matching grants in migrations.
GRANT SELECT ON gram.attribute_metrics_summaries TO marts_definer;

-- Last 12 completed UTC weeks only. Counts span all organizations and are per surface:
-- a person using multiple surfaces counts in each, so rows are not additive.
-- Unknown surface strings are never published verbatim.
CREATE VIEW IF NOT EXISTS marts.weekly_ai_surface_adoption
DEFINER = marts_definer SQL SECURITY DEFINER
AS
WITH lowerUTF8(trimBoth(user_email)) AS normalized_email
SELECT
    toMonday(time_bucket) AS week_start,
    if(
        hook_source IN ('claude-code', 'claude-code-web', 'claude-chat', 'claude-chat-web', 'chatgpt', 'chatgpt-work', 'codex', 'cowork', 'cursor', 'litellm', 'local', 'mcp', 'openclaw', 'opencode'),
        hook_source,
        'other'
    ) AS surface,
    uniqExactIf(normalized_email, normalized_email != '') AS active_users
FROM gram.attribute_metrics_summaries
WHERE is_active = 1
  AND time_bucket >= toDateTime(toMonday(now('UTC')) - INTERVAL 12 WEEK, 'UTC')
  AND time_bucket < toDateTime(toMonday(now('UTC')), 'UTC')
  AND hook_source NOT IN ('', 'assistants', 'chat-analysis', 'elements', 'gram', 'mcp-research', 'playground', 'risk-analysis', 'skill-efficacy', 'skill-suggestions', 'slack')
GROUP BY week_start, surface
HAVING active_users >= 10;

GRANT SELECT ON marts.weekly_ai_surface_adoption TO marts_reader;
