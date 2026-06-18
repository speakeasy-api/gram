-- Rebuild trace_summaries so existing in-window traces gain the tool-usage
-- classification + identity columns (toolset_slug, external_user_id, user_id,
-- mcp_match, mcp_server_url) added alongside this MV change. The MV only
-- populates these for new inserts, so without this backfill pre-migration
-- traces would show blank target/user classification until the 30-day raw TTL
-- rolls them off.
--
-- Clear anything the recreated MV captured between its creation and this
-- backfill, then rebuild from the raw logs (bounded by the 30-day TTL — older
-- history is unrecoverable, which is why this summary table exists going
-- forward).
TRUNCATE TABLE trace_summaries;

-- Historical hosted MCP / direct API tool-call logs were written with a NULL
-- trace_id (before ToolProxy.Do began recording the gateway span's trace
-- context). They would otherwise be excluded from the trace_id-keyed view, so
-- synthesize a per-log trace_id from the log id (a UUID → 32 hex chars, the
-- FixedString(32) width) for traceless tool-call rows. Each such log is one
-- tool call, so one synthetic trace_id per row is correct. New data carries a
-- real trace_id and flows through the MV unchanged.
INSERT INTO trace_summaries
SELECT
    if(trace_id IS NOT NULL AND trace_id != '', trace_id, replaceAll(toString(id), '-', '')) AS trace_id,
    gram_project_id,
    any(gram_deployment_id) AS gram_deployment_id,
    any(gram_function_id) AS gram_function_id,
    any(gram_urn) AS gram_urn,
    any(tool_name) AS tool_name,
    any(tool_source) AS tool_source,
    any(event_source) AS event_source,
    any(user_email) AS user_email,
    any(hook_source) AS hook_source,
    any(skill_name) AS skill_name,
    any(toolset_slug) AS toolset_slug,
    any(external_user_id) AS external_user_id,
    any(user_id) AS user_id,
    any(toString(attributes.gram.mcp.match)) AS mcp_match,
    any(toString(attributes.gram.mcp.server_url)) AS mcp_server_url,
    min(time_unix_nano) AS start_time_unix_nano,
    toUInt64(count(*)) AS log_count,
    anyIfState(
        toInt32OrNull(toString(attributes.http.response.status_code)),
        toString(attributes.http.response.status_code) != ''
    ) AS http_status_code,
    max(if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)) AS has_result,
    max(if(toString(attributes.gram.hook.error) != '', 1, 0)) AS has_error,
    max(if(toString(attributes.gram.hook.block_reason) != '', 1, 0)) AS has_block,
    anyIf(toString(attributes.gram.hook.block_reason), toString(attributes.gram.hook.block_reason) != '') AS block_reason
FROM telemetry_logs
WHERE NOT startsWith(gram_urn, 'urn:uuid:')
  AND (
    (trace_id IS NOT NULL AND trace_id != '')
    OR startsWith(gram_urn, 'tools:')
  )
GROUP BY trace_id, gram_project_id;
