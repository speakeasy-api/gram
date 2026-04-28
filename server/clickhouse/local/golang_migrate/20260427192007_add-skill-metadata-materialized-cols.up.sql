-- drop "trace_summaries_mv" view
DROP VIEW IF EXISTS `trace_summaries_mv`;

ALTER TABLE `telemetry_logs` ADD COLUMN `skill_scope` String MATERIALIZED toString(attributes.gram.skill.scope) COMMENT 'Skill scope (materialized from attributes.gram.skill.scope).';
ALTER TABLE `telemetry_logs` ADD COLUMN `skill_discovery_root` String MATERIALIZED toString(attributes.gram.skill.discovery_root) COMMENT 'Skill discovery root (materialized from attributes.gram.skill.discovery_root).';
ALTER TABLE `telemetry_logs` ADD COLUMN `skill_source_type` String MATERIALIZED toString(attributes.gram.skill.source_type) COMMENT 'Skill source type (materialized from attributes.gram.skill.source_type).';
ALTER TABLE `telemetry_logs` ADD COLUMN `skill_id` String MATERIALIZED toString(attributes.gram.skill.id) COMMENT 'Skill ID (materialized from attributes.gram.skill.id).';
ALTER TABLE `telemetry_logs` ADD COLUMN `skill_version_id` String MATERIALIZED toString(attributes.gram.skill.version_id) COMMENT 'Skill version ID (materialized from attributes.gram.skill.version_id).';
ALTER TABLE `telemetry_logs` ADD COLUMN `skill_resolution_status` String MATERIALIZED toString(attributes.gram.skill.resolution_status) COMMENT 'Skill resolution status (materialized from attributes.gram.skill.resolution_status).';

ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_skill_scope` ((skill_scope)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_skill_discovery_root` ((skill_discovery_root)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_skill_source_type` ((skill_source_type)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_skill_id` ((skill_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_skill_version_id` ((skill_version_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `telemetry_logs` ADD INDEX `idx_telemetry_logs_mat_skill_resolution_status` ((skill_resolution_status)) TYPE bloom_filter(0.01) GRANULARITY 1;

ALTER TABLE `trace_summaries` ADD COLUMN `skill_scope` SimpleAggregateFunction(any, String);
ALTER TABLE `trace_summaries` ADD COLUMN `skill_discovery_root` SimpleAggregateFunction(any, String);
ALTER TABLE `trace_summaries` ADD COLUMN `skill_source_type` SimpleAggregateFunction(any, String);
ALTER TABLE `trace_summaries` ADD COLUMN `skill_id` SimpleAggregateFunction(any, String);
ALTER TABLE `trace_summaries` ADD COLUMN `skill_version_id` SimpleAggregateFunction(any, String);
ALTER TABLE `trace_summaries` ADD COLUMN `skill_resolution_status` SimpleAggregateFunction(any, String);

-- create "trace_summaries_mv" view
CREATE MATERIALIZED VIEW `trace_summaries_mv` TO `trace_summaries` AS SELECT trace_id, gram_project_id, any(gram_deployment_id) AS gram_deployment_id, any(gram_function_id) AS gram_function_id, any(gram_urn) AS gram_urn, any(tool_name) AS tool_name, any(tool_source) AS tool_source, any(event_source) AS event_source, any(user_email) AS user_email, any(hook_source) AS hook_source, any(skill_name) AS skill_name, any(skill_scope) AS skill_scope, any(skill_discovery_root) AS skill_discovery_root, any(skill_source_type) AS skill_source_type, any(skill_id) AS skill_id, any(skill_version_id) AS skill_version_id, any(skill_resolution_status) AS skill_resolution_status, min(time_unix_nano) AS start_time_unix_nano, toUInt64(count(*)) AS log_count, anyIfState(toInt32OrNull(toString(attributes.http.response.status_code)), toString(attributes.http.response.status_code) != '') AS http_status_code, max(if(toString(attributes.gen_ai.tool.call.result) != '', 1, 0)) AS has_result, max(if(toString(attributes.gram.hook.error) != '', 1, 0)) AS has_error, max(if(toString(attributes.gram.hook.block_reason) != '', 1, 0)) AS has_block, anyIf(toString(attributes.gram.hook.block_reason), toString(attributes.gram.hook.block_reason) != '') AS block_reason FROM telemetry_logs WHERE (trace_id IS NOT NULL) AND (trace_id != '') AND (NOT startsWith(telemetry_logs.gram_urn, 'urn:uuid:')) GROUP BY trace_id, gram_project_id;
