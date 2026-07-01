ALTER TABLE `chat_turn_summaries` MODIFY TTL fromUnixTimestamp64Nano(start_time_unix_nano) + toIntervalDay(30);
