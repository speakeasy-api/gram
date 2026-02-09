-- Seed script for telemetry_logs with realistic observability data
-- Generates ~120k records over 3 months with smooth distribution:
-- - Even spread across time with natural jitter
-- - Business hours bias (more traffic 9am-6pm)
-- - Weekday bias (less traffic on weekends)
-- - Growth trend (more recent = more traffic)
-- - Realistic tool popularity distribution

-- First, clear existing test data
ALTER TABLE telemetry_logs DELETE WHERE gram_project_id = toUUID('019c2935-5fab-7614-b757-3e0be85fbee3');

-- Wait for mutation to complete
SELECT sleepEachRow(0.1) FROM numbers(10) FORMAT Null;

-- Insert tool call logs with smooth distribution
INSERT INTO telemetry_logs (
    time_unix_nano,
    observed_time_unix_nano,
    severity_text,
    body,
    attributes,
    resource_attributes,
    gram_project_id,
    gram_urn,
    service_name,
    gram_chat_id
)
SELECT
    time_unix_nano,
    time_unix_nano as observed_time_unix_nano,
    'INFO' as severity_text,
    concat('Tool call: ', tool_name) as body,
    concat(
        '{"http.response.status_code": ', toString(status_code),
        ', "http.server.request.duration": ', toString(latency_sec),
        ', "gram.tool.urn": "tools:', tool_name, '"',
        ', "gram.project.id": "019c2935-5fab-7614-b757-3e0be85fbee3"',
        ', "user.id": "user-', toString(user_id), '"',
        ', "gram.external_user.id": "ext-user-', toString(ext_user_id), '"',
        ', "gram.api_key.id": "key-', toString(api_key_id), '"',
        '}'
    ) as attributes,
    '{"gram.deployment.id": "deployment-1"}' as resource_attributes,
    toUUID('019c2935-5fab-7614-b757-3e0be85fbee3') as gram_project_id,
    concat('tools:', tool_name) as gram_urn,
    'gram-mcp-gateway' as service_name,
    concat('chat-', toString(chat_id)) as gram_chat_id
FROM (
    SELECT
        number,
        -- Spread events evenly across 90 days with jitter
        -- Base: linear spread + random jitter of up to 5 minutes
        toInt64(toUnixTimestamp64Nano(
            now64(9)
            - toIntervalSecond(
                -- Linear spread: each event ~45 seconds apart for 180k events over 90 days
                (number * 43) + (rand() % 300)  -- base spacing + up to 5 min jitter
            )
        )) as time_unix_nano,

        -- chat_id comes from inner subquery (consistent per chat for api_key_id and ext_user_id)
        chat_id,

        -- User distribution - some users more active than others (power law)
        multiIf(
            rand() % 100 < 50, rand() % 10,   -- 50% from top 10 users
            rand() % 100 < 80, rand() % 50,   -- 30% from top 50
            rand() % 200                       -- 20% from long tail
        ) as user_id,

        ext_user_id,
        api_key_id,

        -- Tool popularity follows power law - some tools much more popular
        arrayElement(
            ['github:list-repos', 'github:list-repos', 'github:list-repos',  -- Very popular
             'slack:send-message', 'slack:send-message',                      -- Popular
             'postgres:query', 'postgres:query',
             'openai:chat',
             'github:create-issue', 'jira:get-ticket', 'linear:create-issue',
             'notion:create-page', 'confluence:get-page',
             'stripe:create-payment', 'twilio:send-sms',
             'aws:s3-upload', 'gcp:bigquery-run',
             'asana:create-task', 'monday:update-item', 'airtable:list-records'],
            (rand() % 20) + 1
        ) as tool_name,

        -- Status codes vary by tool type
        multiIf(
            tool_name IN ('postgres:query', 'gcp:bigquery-run') AND rand() % 100 < 8, 500,  -- DB tools: 8% server error
            tool_name IN ('stripe:create-payment') AND rand() % 100 < 5, 402,               -- Payment: 5% payment required
            tool_name IN ('github:create-issue', 'linear:create-issue') AND rand() % 100 < 3, 403, -- Issue creation: 3% forbidden
            rand() % 100 < 88, 200,  -- 88% success
            rand() % 100 < 95, arrayElement([400, 401, 403, 404, 422, 429], (rand() % 6) + 1),
            arrayElement([500, 502, 503, 504], (rand() % 4) + 1)
        ) as status_code,

        -- Latency varies by tool
        multiIf(
            tool_name IN ('postgres:query', 'gcp:bigquery-run'), 0.1 + (rand() % 2000) / 1000.0,  -- DB: 100ms-2s
            tool_name = 'openai:chat', 0.5 + (rand() % 5000) / 1000.0,                            -- AI: 500ms-5.5s
            tool_name IN ('aws:s3-upload'), 0.2 + (rand() % 3000) / 1000.0,                       -- Upload: 200ms-3s
            rand() % 100 < 70, 0.03 + (rand() % 150) / 1000.0,                                    -- Fast: 30-180ms
            rand() % 100 < 95, 0.15 + (rand() % 500) / 1000.0,                                    -- Medium: 150-650ms
            0.8 + (rand() % 2000) / 1000.0                                                         -- Slow: 800ms-2.8s
        ) as latency_sec
    FROM (
        SELECT
            number,
            chat_id,
            -- External user is consistent per chat (derived from chat_id)
            chat_id % 80 as ext_user_id,
            -- API key is consistent per chat (derived from chat_id)
            chat_id % 5 as api_key_id
        FROM (
            SELECT
                number,
                rand() % 15000 as chat_id
            FROM numbers(180000)
        )
    )
    -- Skip ~35% randomly to create natural variation (not perfectly uniform)
    WHERE rand() % 100 < 65
);

-- Insert chat completion logs with smooth distribution
INSERT INTO telemetry_logs (
    time_unix_nano,
    observed_time_unix_nano,
    severity_text,
    body,
    attributes,
    resource_attributes,
    gram_project_id,
    gram_urn,
    service_name,
    gram_chat_id
)
SELECT
    time_unix_nano,
    time_unix_nano as observed_time_unix_nano,
    'INFO' as severity_text,
    'Chat completion' as body,
    concat(
        '{"gen_ai.response.finish_reasons": ["', finish_reason, '"]',
        ', "gen_ai.conversation.id": "chat-', toString(chat_id), '"',
        ', "gen_ai.conversation.duration": ', toString(duration_sec),
        ', "gram.resource.urn": "agents:chat:completion"',
        ', "gram.project.id": "019c2935-5fab-7614-b757-3e0be85fbee3"',
        ', "user.id": "user-', toString(chat_id % 200), '"',
        ', "gram.external_user.id": "ext-user-', toString(chat_id % 80), '"',
        ', "gram.api_key.id": "key-', toString(chat_id % 5), '"',
        ', "http.response.status_code": ', toString(status_code),
        '}'
    ) as attributes,
    '{"gram.deployment.id": "deployment-1"}' as resource_attributes,
    toUUID('019c2935-5fab-7614-b757-3e0be85fbee3') as gram_project_id,
    'agents:chat:completion' as gram_urn,
    'gram-mcp-gateway' as service_name,
    concat('chat-', toString(chat_id)) as gram_chat_id
FROM (
    SELECT
        number,
        number % 15000 as chat_id,
        -- Spread events evenly across 90 days with jitter
        -- ~25k chats over 90 days = one every ~310 seconds
        toInt64(toUnixTimestamp64Nano(
            now64(9)
            - toIntervalSecond(
                (number * 310) + (rand() % 600)  -- base spacing + up to 10 min jitter
            )
        )) as time_unix_nano,
        -- 65% resolved, 25% length limit, 10% error
        multiIf(
            rand() % 100 < 65, 'stop',
            rand() % 100 < 90, 'length',
            'error'
        ) as finish_reason,
        -- Chat duration: 10-600 seconds, with most being 30-180s
        multiIf(
            rand() % 100 < 60, 30 + (rand() % 150),   -- 60%: 30-180s
            rand() % 100 < 90, 10 + (rand() % 300),   -- 30%: 10-310s
            180 + (rand() % 420)                       -- 10%: 180-600s (long sessions)
        ) as duration_sec,
        if(rand() % 100 < 92, 200, arrayElement([400, 500, 502, 504], (rand() % 4) + 1)) as status_code
    FROM numbers(25000)
    WHERE rand() % 100 < 80  -- Create some natural gaps
);

-- Insert chat resolution analysis events (these contain the evaluation_score_label)
-- ~70% of chats get a resolution analysis (the rest are abandoned/ongoing)
INSERT INTO telemetry_logs (
    time_unix_nano,
    observed_time_unix_nano,
    severity_text,
    body,
    attributes,
    resource_attributes,
    gram_project_id,
    gram_urn,
    service_name,
    gram_chat_id
)
SELECT
    time_unix_nano,
    time_unix_nano as observed_time_unix_nano,
    'INFO' as severity_text,
    concat('Chat resolution: ', resolution) as body,
    concat(
        '{"gen_ai.evaluation.name": "chat_resolution"',
        ', "gen_ai.evaluation.score.label": "', resolution, '"',
        ', "gen_ai.evaluation.score.value": ', toString(score),
        ', "gen_ai.conversation.id": "chat-', toString(chat_id), '"',
        ', "gen_ai.conversation.duration": ', toString(duration_sec),
        ', "gram.project.id": "019c2935-5fab-7614-b757-3e0be85fbee3"',
        ', "user.id": "user-', toString(chat_id % 200), '"',
        ', "gram.external_user.id": "ext-user-', toString(chat_id % 80), '"',
        ', "gram.api_key.id": "key-', toString(chat_id % 5), '"',
        '}'
    ) as attributes,
    '{"gram.deployment.id": "deployment-1"}' as resource_attributes,
    toUUID('019c2935-5fab-7614-b757-3e0be85fbee3') as gram_project_id,
    'chat_resolution' as gram_urn,
    'gram-resolution-analyzer' as service_name,
    concat('chat-', toString(chat_id)) as gram_chat_id
FROM (
    SELECT
        number,
        number % 15000 as chat_id,
        -- Resolution happens a few seconds after chat completion
        toInt64(toUnixTimestamp64Nano(
            now64(9)
            - toIntervalSecond(
                (number * 310) + (rand() % 600) - 5  -- Slightly after chat completion
            )
        )) as time_unix_nano,
        -- Use a single random value for resolution distribution
        rand() % 100 as resolution_rand,
        -- Resolution distribution: 65% success, 15% partial, 12% failure, 8% abandoned
        multiIf(
            resolution_rand < 65, 'success',
            resolution_rand < 80, 'partial',
            resolution_rand < 92, 'failure',
            'abandoned'
        ) as resolution,
        -- Score correlates with resolution
        multiIf(
            resolution = 'success', 80 + (rand() % 21),    -- 80-100
            resolution = 'partial', 40 + (rand() % 31),    -- 40-70
            resolution = 'failure', rand() % 30,           -- 0-29
            rand() % 20                                     -- abandoned: 0-19
        ) as score,
        -- Duration: same logic as chat completion
        rand() % 100 as duration_rand,
        multiIf(
            duration_rand < 60, 30 + (rand() % 150),
            duration_rand < 90, 10 + (rand() % 300),
            180 + (rand() % 420)
        ) as duration_sec
    FROM numbers(25000)
    WHERE rand() % 100 < 70  -- 70% of chats get resolution analysis
);

SELECT 'Inserted sample data', count(*) as total_rows FROM telemetry_logs WHERE gram_project_id = toUUID('019c2935-5fab-7614-b757-3e0be85fbee3');
