ALTER TABLE `gram`.`trace_summaries` ADD INDEX `idx_trace_summaries_agent_id` ((agent_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
-- Materialize index "idx_trace_summaries_agent_id" for existing data
ALTER TABLE `gram`.`trace_summaries` MATERIALIZE INDEX `idx_trace_summaries_agent_id`;
