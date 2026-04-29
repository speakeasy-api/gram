ALTER TABLE `trace_summaries` MODIFY COLUMN `block_reason` SimpleAggregateFunction(any, String);
