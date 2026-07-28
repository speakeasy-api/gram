ALTER TABLE `attribute_keys` MODIFY TTL fromUnixTimestamp64Nano(last_seen_unix_nano) + toIntervalDay(90);
ALTER TABLE `attribute_metrics_summaries` MODIFY TTL time_bucket + toIntervalDay(90);
ALTER TABLE `metrics_summaries` MODIFY TTL time_bucket + toIntervalDay(90);
ALTER TABLE `telemetry_logs` MODIFY TTL fromUnixTimestamp64Nano(time_unix_nano) + toIntervalDay(90);
ALTER TABLE `trace_summaries` MODIFY TTL fromUnixTimestamp64Nano(start_time_unix_nano) + toIntervalDay(90);
