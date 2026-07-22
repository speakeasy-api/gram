ALTER TABLE `trace_summaries` ADD INDEX `idx_trace_summaries_start_time` ((start_time_unix_nano)) TYPE minmax GRANULARITY 1;
-- Build the index for existing parts as a background mutation -- ADD INDEX
-- alone only covers parts written after it. Index-only rebuild: no data
-- rewrite.
ALTER TABLE `trace_summaries` MATERIALIZE INDEX `idx_trace_summaries_start_time`;
