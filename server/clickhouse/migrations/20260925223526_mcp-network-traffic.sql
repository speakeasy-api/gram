-- Create "mcp_network_traffic_hourly_summaries" table
CREATE TABLE `gram`.`mcp_network_traffic_hourly_summaries` (
  `gram_project_id` UUID COMMENT 'Project that received the inbound MCP request.',
  `hour` DateTime COMMENT 'UTC hour containing the observed request.',
  `server_kind` LowCardinality(String) COMMENT 'MCP server kind: mcp or meta.',
  `server_id` String COMMENT 'ID of the MCP server or meta MCP server.',
  `surface` LowCardinality(String) COMMENT 'Network surface: public or private.',
  `request_count` SimpleAggregateFunction(sum, UInt64),
  `last_seen` SimpleAggregateFunction(max, DateTime64(9))
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`gram_project_id`, `server_kind`, `server_id`, `surface`, `hour`) ORDER BY (`gram_project_id`, `server_kind`, `server_id`, `surface`, `hour`) PARTITION BY (toYYYYMM(hour)) TTL hour + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Hourly counts of observed inbound MCP HTTP requests by server and network surface';
-- Create "mcp_network_traffic_hourly_summaries_mv" view
CREATE MATERIALIZED VIEW `gram`.`mcp_network_traffic_hourly_summaries_mv` TO `gram`.`mcp_network_traffic_hourly_summaries` AS SELECT gram_project_id, toStartOfHour(observed_timestamp, 'UTC') AS hour, if(mcp_server_id != '', 'mcp', 'meta') AS server_kind, if(mcp_server_id != '', mcp_server_id, meta_mcp_server_id) AS server_id, toLowCardinality(toString(attributes.gram.network.surface)) AS surface, toUInt64(count()) AS request_count, max(observed_timestamp) AS last_seen FROM gram.telemetry_logs WHERE (event_urn = 'urn:telemetry:gram_service:log:mcp_network_request') AND ((mcp_server_id != '') OR (meta_mcp_server_id != '')) AND (surface IN ('public', 'private')) GROUP BY gram_project_id, hour, server_kind, server_id, surface;
