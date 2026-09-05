-- The marts database is an allowlist boundary for employee and agent analytics.
-- Keep it view-only. Never expose raw identifiers, free-form customer content,
-- customer-defined names, or monetary values here. Every published row must
-- represent at least ten distinct identified users.
CREATE DATABASE IF NOT EXISTS marts ENGINE = Atomic;

-- Atlas currently ignores access-control statements when loading SQL desired
-- state. Keep them here as the complete contract and bootstrap them with a
-- manual migration.
CREATE ROLE IF NOT EXISTS marts_reader SETTINGS
    readonly = 1 CONST,
    max_execution_time = 30 CONST,
    max_memory_usage = 2000000000 CONST,
    max_rows_to_read = 100000000 CONST,
    max_bytes_to_read = 5000000000 CONST,
    max_threads = 4 CONST,
    max_result_rows = 10000 CONST,
    max_result_bytes = 10000000 CONST,
    result_overflow_mode = 'throw' CONST,
    max_concurrent_queries_for_user = 4 CONST;

CREATE USER IF NOT EXISTS marts_definer HOST NONE;

GRANT SELECT ON gram.attribute_metrics_summaries TO marts_definer;

GRANT SELECT ON marts.* TO marts_reader;

-- Completed UTC weeks only. Counts span all organizations and are per surface:
-- a person using multiple surfaces counts in each, so rows are not additive.
-- Unknown surface strings are never published verbatim.
CREATE VIEW IF NOT EXISTS marts.weekly_ai_surface_adoption
DEFINER = marts_definer SQL SECURITY DEFINER
AS
SELECT
    toMonday(time_bucket) AS week_start,
    if(
        hook_source IN ('claude-code', 'codex', 'cowork', 'cursor', 'litellm', 'local', 'mcp', 'openclaw', 'opencode'),
        hook_source,
        'other'
    ) AS surface,
    uniqExactIf(user_email, user_email != '') AS active_users
FROM gram.attribute_metrics_summaries
WHERE is_active = 1
  AND time_bucket < toDateTime(toMonday(now('UTC')), 'UTC')
  AND hook_source NOT IN ('', 'assistants', 'chat-analysis', 'elements', 'gram', 'mcp-research', 'playground', 'risk-analysis', 'skill-efficacy', 'skill-suggestions', 'slack')
GROUP BY week_start, surface
HAVING active_users >= 10;
