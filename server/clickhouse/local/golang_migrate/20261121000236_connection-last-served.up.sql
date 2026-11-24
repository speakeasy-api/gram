-- create "mcp_connection_last_served" table
CREATE TABLE `gram`.`mcp_connection_last_served` (
  `organization_id` String COMMENT 'Organization the connection belongs to. Readers always filter on it.
@access: confidential' CODEC(ZSTD(1)),
  `project_id` UUID COMMENT 'Project of the server, nil for an organization gateway.
@access: confidential',
  `server_kind` LowCardinality(String) COMMENT 'Kind of server the request was for, such as mcp, meta or toolset.
@access: confidential',
  `server_id` String COMMENT 'ID of the MCP server, gateway or toolset the request was for.
@access: confidential' CODEC(ZSTD(1)),
  `member_id` String COMMENT 'Gateway member MCP server ID when the request was for one member, empty otherwise.
@access: confidential' CODEC(ZSTD(1)),
  `subject_urn` String COMMENT 'Session subject URN of the user, API key, agent or workload the request was served for. Never empty or anonymous.
@access: confidential-pii' CODEC(ZSTD(1)),
  `app_key` String COMMENT 'App the request came through, as on mcp_connection_events.
@access: restricted',
  `client_family` LowCardinality(String) COMMENT 'Kind of client from the User-Agent, as on mcp_connection_events.
@access: confidential',
  `remote_session_client_id` String COMMENT 'ID in remote_session_clients of the service sign-in of this person that the request used, empty when it used none of theirs.
@access: confidential' CODEC(ZSTD(1)),
  `last_served_at` SimpleAggregateFunction(max, DateTime64(9, 'UTC')) COMMENT 'Latest time a request on this connection was served without an error, whoever answered it.
@access: confidential',
  `last_answered_at` SimpleAggregateFunction(max, DateTime64(9, 'UTC')) COMMENT 'Latest time the server itself answered a request on this connection without an error. The epoch when only Speakeasy has answered.
@access: confidential'
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `server_kind`, `server_id`, `member_id`, `subject_urn`, `app_key`, `client_family`, `remote_session_client_id`, `project_id`) ORDER BY (`organization_id`, `server_kind`, `server_id`, `member_id`, `subject_urn`, `app_key`, `client_family`, `remote_session_client_id`, `project_id`) TTL toDateTime(last_served_at) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'When each MCP connection last had a request served: one row per person, server, gateway member, app, client and service sign-in. A write for the same connection folds into its row, so no history is kept. Read with max() per connection. A connection not served for 90 days is dropped.';
