-- Create "tunnel_metric_snapshots" table
CREATE TABLE `gram`.`tunnel_metric_snapshots` (
  `gram_project_id` UUID COMMENT 'Project that owns the tunneled MCP source.
@access: confidential',
  `source_id` UUID COMMENT 'Tunneled MCP source the snapshot describes.
@access: confidential',
  `bucket` DateTime('UTC') COMMENT 'Start of the minute or 15-second sample interval.
@access: confidential',
  `kind` LowCardinality(String) COMMENT 'Snapshot kind, requests or connections.
@access: confidential',
  `producer_id` UUID COMMENT 'Tunnel gateway process that produced the snapshot.
@access: confidential',
  `server_id` UUID COMMENT 'MCP server the requests targeted, nil for connection samples.
@access: confidential',
  `method` LowCardinality(String) COMMENT 'MCP method from a fixed allowlist, other for anything else.
@access: confidential',
  `client_family` LowCardinality(String) COMMENT 'Fixed client family label derived from the MCP client name.
@access: confidential',
  `revision` UInt64 COMMENT 'Producer revision. The highest revision of a key replaces earlier ones.
@access: confidential',
  `attempts` UInt64 COMMENT 'Requests started in the interval.
@access: confidential',
  `successes` UInt64 COMMENT 'Requests that completed with an MCP response.
@access: confidential',
  `errors` UInt64 COMMENT 'Requests that completed with an MCP error.
@access: confidential',
  `canceled` UInt64 COMMENT 'Requests canceled before completion.
@access: confidential',
  `incomplete` UInt64 COMMENT 'Requests still in flight or abandoned when the snapshot was taken.
@access: confidential',
  `latency_bins` Array(UInt64) COMMENT 'Request counts per fixed latency histogram bin.
@access: confidential',
  `connections` UInt32 COMMENT 'Connected tunnel agents at the sample time.
@access: confidential',
  `consumers` UInt32 COMMENT 'Client sessions open through the tunnel at the sample time.
@access: confidential',
  `substreams` UInt32 COMMENT 'Requests in flight through the tunnel at the sample time.
@access: confidential',
  `connections_opened` UInt64 COMMENT 'Agent connections opened since the previous sample.
@access: confidential'
) ENGINE = ReplacingMergeTree(revision)
PRIMARY KEY (`gram_project_id`, `source_id`, `bucket`, `kind`, `producer_id`, `server_id`, `method`, `client_family`) ORDER BY (`gram_project_id`, `source_id`, `bucket`, `kind`, `producer_id`, `server_id`, `method`, `client_family`) PARTITION BY (toDate(bucket)) TTL bucket + toIntervalDay(7) SETTINGS index_granularity = 8192;
