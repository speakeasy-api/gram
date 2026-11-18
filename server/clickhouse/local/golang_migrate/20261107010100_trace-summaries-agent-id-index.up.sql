ALTER TABLE `gram`.`trace_summaries` ADD INDEX `idx_trace_summaries_agent_id` ((agent_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
-- Build the index for existing parts as a background mutation -- ADD INDEX
-- alone only covers parts written after it. Index-only rebuild: no data
-- rewrite.
ALTER TABLE `gram`.`trace_summaries` MATERIALIZE INDEX `idx_trace_summaries_agent_id`;
