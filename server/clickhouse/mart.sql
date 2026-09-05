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

-- Daily adoption of externally observed AI clients. Gram-hosted inference is
-- excluded so platform work is not attributed to customer users.
CREATE VIEW IF NOT EXISTS marts.daily_agent_adoption
DEFINER = marts_definer SQL SECURITY DEFINER
AS
SELECT
    toDate(time_bucket) AS usage_date,
    multiIf(
        hook_source IN ('claude-code', 'codex', 'cowork', 'cursor', 'litellm', 'local', 'mcp', 'openclaw', 'opencode'), hook_source,
        'other'
    ) AS surface,
    if(account_type = 'personal', 'personal', 'team') AS account_kind,
    uniqExactIf(user_email, user_email != '') AS active_users,
    uniqExactIfMerge(total_chats) AS conversations,
    toInt64(sumIfMerge(total_input_tokens)) AS input_tokens,
    toInt64(sumIfMerge(total_output_tokens)) AS output_tokens,
    toInt64(sumIfMerge(cache_read_input_tokens)) AS cache_read_tokens,
    toInt64(sumIfMerge(cache_creation_input_tokens)) AS cache_creation_tokens,
    toInt64(sumIfMerge(total_input_tokens) + sumIfMerge(total_output_tokens)) AS llm_tokens,
    toInt64(sumIfMerge(total_input_tokens) + sumIfMerge(total_output_tokens) + sumIfMerge(cache_creation_input_tokens)) AS managed_tokens,
    toUInt64(if(
        uniqExactIfMerge(unique_tool_calls) = 0,
        countIfMerge(total_tool_calls),
        uniqExactIfMerge(unique_tool_calls)
    )) AS tool_calls
FROM gram.attribute_metrics_summaries
WHERE is_active = 1
  AND hook_source NOT IN ('', 'assistants', 'chat-analysis', 'elements', 'gram', 'mcp-research', 'playground', 'risk-analysis', 'skill-efficacy', 'skill-suggestions', 'slack')
GROUP BY usage_date, surface, account_kind
HAVING active_users >= 10;

-- Daily model adoption. Exact model names are retained only for cohorts large
-- enough to satisfy the same privacy threshold.
CREATE VIEW IF NOT EXISTS marts.daily_model_usage
DEFINER = marts_definer SQL SECURITY DEFINER
AS
SELECT
    toDate(time_bucket) AS usage_date,
    multiIf(
        provider IN ('anthropic', 'aws-bedrock', 'azure-openai', 'cursor', 'google', 'openai', 'openrouter'), provider,
        provider = '', 'unknown',
        'other'
    ) AS provider_name,
    model AS model_name,
    uniqExactIf(user_email, user_email != '') AS active_users,
    uniqExactIfMerge(total_chats) AS conversations,
    toInt64(sumIfMerge(total_input_tokens)) AS input_tokens,
    toInt64(sumIfMerge(total_output_tokens)) AS output_tokens,
    toInt64(sumIfMerge(cache_read_input_tokens)) AS cache_read_tokens,
    toInt64(sumIfMerge(cache_creation_input_tokens)) AS cache_creation_tokens,
    toInt64(sumIfMerge(total_input_tokens) + sumIfMerge(total_output_tokens)) AS llm_tokens,
    toInt64(sumIfMerge(total_input_tokens) + sumIfMerge(total_output_tokens) + sumIfMerge(cache_creation_input_tokens)) AS managed_tokens
FROM gram.attribute_metrics_summaries
WHERE is_active = 1
  AND model != ''
  AND hook_source NOT IN ('', 'assistants', 'chat-analysis', 'elements', 'gram', 'mcp-research', 'playground', 'risk-analysis', 'skill-efficacy', 'skill-suggestions', 'slack')
GROUP BY usage_date, provider_name, model_name
HAVING active_users >= 10;

-- Daily adoption by coarse job function. Free-form department names and job
-- titles are used only inside the definer query and never leave the view.
CREATE VIEW IF NOT EXISTS marts.daily_function_adoption
DEFINER = marts_definer SQL SECURITY DEFINER
AS
WITH lowerUTF8(concat(department_name, ' ', job_title)) AS employee_context
SELECT
    toDate(time_bucket) AS usage_date,
    multiIf(
        employee_context = ' ', 'unknown',
        match(employee_context, '(engineering|engineer|developer|software|platform|infrastructure|devops|sre|security|quality|qa|data)'), 'engineering',
        match(employee_context, '(product|design|research|ux|user experience)'), 'product_design',
        match(employee_context, '(sales|business development|revenue|account executive|solutions)'), 'sales',
        match(employee_context, '(marketing|growth|brand|content|communications)'), 'marketing',
        match(employee_context, '(customer success|customer support|support|services)'), 'customer_success',
        match(employee_context, '(finance|legal|people|human resources|operations|recruit|talent)'), 'business_operations',
        'other'
    ) AS employee_function,
    uniqExactIf(user_email, user_email != '') AS active_users,
    uniqExactIfMerge(total_chats) AS conversations,
    toInt64(sumIfMerge(total_input_tokens)) AS input_tokens,
    toInt64(sumIfMerge(total_output_tokens)) AS output_tokens,
    toInt64(sumIfMerge(cache_read_input_tokens)) AS cache_read_tokens,
    toInt64(sumIfMerge(cache_creation_input_tokens)) AS cache_creation_tokens,
    toInt64(sumIfMerge(total_input_tokens) + sumIfMerge(total_output_tokens)) AS llm_tokens,
    toInt64(sumIfMerge(total_input_tokens) + sumIfMerge(total_output_tokens) + sumIfMerge(cache_creation_input_tokens)) AS managed_tokens,
    toUInt64(if(
        uniqExactIfMerge(unique_tool_calls) = 0,
        countIfMerge(total_tool_calls),
        uniqExactIfMerge(unique_tool_calls)
    )) AS tool_calls
FROM gram.attribute_metrics_summaries
WHERE is_active = 1
  AND hook_source NOT IN ('', 'assistants', 'chat-analysis', 'elements', 'gram', 'mcp-research', 'playground', 'risk-analysis', 'skill-efficacy', 'skill-suggestions', 'slack')
GROUP BY usage_date, employee_function
HAVING active_users >= 10;
