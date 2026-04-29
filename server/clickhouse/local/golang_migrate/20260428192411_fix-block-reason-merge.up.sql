ALTER TABLE `trace_summaries` MODIFY COLUMN `block_reason` SimpleAggregateFunction(max, String);
