-- create "tunnel_metric_snapshots" table
CREATE TABLE `gram`.`tunnel_metric_snapshots` (
  `gram_project_id` UUID,
  `source_id` UUID,
  `bucket` DateTime('UTC'),
  `kind` LowCardinality(String),
  `producer_id` UUID,
  `server_id` UUID,
  `method` LowCardinality(String),
  `client_family` LowCardinality(String),
  `revision` UInt64,
  `attempts` UInt64,
  `successes` UInt64,
  `errors` UInt64,
  `canceled` UInt64,
  `incomplete` UInt64,
  `latency_bins` Array(UInt64),
  `connections` UInt32,
  `consumers` UInt32,
  `substreams` UInt32,
  `connections_opened` UInt64
) ENGINE = ReplacingMergeTree(revision)
PRIMARY KEY (`gram_project_id`, `source_id`, `bucket`, `kind`, `producer_id`, `server_id`, `method`, `client_family`) ORDER BY (`gram_project_id`, `source_id`, `bucket`, `kind`, `producer_id`, `server_id`, `method`, `client_family`) PARTITION BY (toDate(bucket)) TTL bucket + toIntervalDay(7) SETTINGS index_granularity = 8192;
