ALTER TABLE `gram`.`sigint_readings_v2` ADD INDEX `event_occurred_at_minmax_idx` ((event_occurred_at)) TYPE minmax;
-- materialize index "event_occurred_at_minmax_idx" for existing data
ALTER TABLE `gram`.`sigint_readings_v2` MATERIALIZE INDEX `event_occurred_at_minmax_idx`;
