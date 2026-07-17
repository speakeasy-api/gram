ALTER TABLE `attribute_metrics_summaries` MODIFY TTL time_bucket + toIntervalDay(90);
