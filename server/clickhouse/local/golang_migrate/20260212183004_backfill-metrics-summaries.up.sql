-- Backfill metrics_summaries with existing telemetry_logs data
INSERT INTO metrics_summaries
SELECT
    gram_project_id,
    toStartOfHour(fromUnixTimestamp64Nano(time_unix_nano)) AS time_bucket,

    -- Activity timestamps
    min(time_unix_nano) AS first_seen_unix_nano,
    max(time_unix_nano) AS last_seen_unix_nano,

    -- Cardinality
    uniqExactIfState(toString(attributes.gen_ai.conversation.id), toString(attributes.gen_ai.conversation.id) != '') AS total_chats,
    uniqExactIfState(toString(attributes.gen_ai.response.model), toString(attributes.gen_ai.response.model) != '') AS distinct_models,
    uniqExactIfState(toString(attributes.gen_ai.provider.name), toString(attributes.gen_ai.provider.name) != '') AS distinct_providers,

    -- Token sums
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_input_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_output_tokens,
    sumIfState(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_tokens,

    -- Avg tokens per request
    avgIfState(toFloat64OrZero(toString(attributes.gen_ai.usage.total_tokens)), toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_tokens_per_request,

    -- Chat request count
    countIfState(toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS total_chat_requests,

    -- Avg chat duration
    avgIfState(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, toString(attributes.gram.resource.urn) = 'agents:chat:completion') AS avg_chat_duration_ms,

    -- Finish reasons
    countIfState(position(toString(attributes.gen_ai.response.finish_reasons), 'stop') > 0) AS finish_reason_stop,
    countIfState(position(toString(attributes.gen_ai.response.finish_reasons), 'tool_calls') > 0) AS finish_reason_tool_calls,

    -- Tool call metrics
    countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS total_tool_calls,
    countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_call_success,
    countIfState(startsWith(toString(attributes.gram.tool.urn), 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_call_failure,
    avgIfState(toFloat64OrZero(toString(attributes.http.server.request.duration)) * 1000, startsWith(toString(attributes.gram.tool.urn), 'tools:')) AS avg_tool_duration_ms,

    -- Chat resolution metrics
    countIfState(evaluation_score_label = 'success') AS chat_resolution_success,
    countIfState(evaluation_score_label = 'failure') AS chat_resolution_failure,
    countIfState(evaluation_score_label = 'partial') AS chat_resolution_partial,
    countIfState(evaluation_score_label = 'abandoned') AS chat_resolution_abandoned,
    avgIfState(toFloat64OrZero(toString(attributes.gen_ai.evaluation.score.value)), evaluation_score_label != '') AS avg_chat_resolution_score,

    -- Overview: evaluated chat counts (distinct chats by resolution status)
    uniqExactIfState(ifNull(toString(gram_chat_id), ''), ifNull(toString(gram_chat_id), '') != '' AND evaluation_score_label != '') AS evaluated_chats,
    uniqExactIfState(ifNull(toString(gram_chat_id), ''), ifNull(toString(gram_chat_id), '') != '' AND evaluation_score_label = 'success') AS resolved_chats,
    uniqExactIfState(ifNull(toString(gram_chat_id), ''), ifNull(toString(gram_chat_id), '') != '' AND evaluation_score_label = 'failure') AS failed_chats,
    avgIfState(toFloat64OrZero(toString(attributes.gen_ai.conversation.duration)) * 1000, evaluation_score_label = 'success') AS avg_resolution_time_ms,

    -- Model breakdown
    sumMapIfState(map(toString(attributes.gen_ai.response.model), toUInt64(1)), toString(attributes.gram.resource.urn) = 'agents:chat:completion' AND toString(attributes.gen_ai.response.model) != '') AS models,

    -- Tool breakdowns
    sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:')) AS tool_counts,
    sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 200 AND toInt32OrZero(toString(attributes.http.response.status_code)) < 300) AS tool_success_counts,
    sumMapIfState(map(gram_urn, toUInt64(1)), startsWith(gram_urn, 'tools:') AND toInt32OrZero(toString(attributes.http.response.status_code)) >= 400) AS tool_failure_counts
FROM telemetry_logs
GROUP BY gram_project_id, time_bucket;
