-- create "shadow_mcp_inventory_urls" table
CREATE TABLE `shadow_mcp_inventory_urls` (
  `gram_project_id` UUID,
  `canonical_server_url` String,
  `url_host` String,
  `server_name` String,
  `first_seen` DateTime64(9, 'UTC'),
  `last_seen` DateTime64(9, 'UTC'),
  `updated_at` DateTime64(9, 'UTC')
) ENGINE = ReplacingMergeTree(updated_at)
PRIMARY KEY (`gram_project_id`, `canonical_server_url`) ORDER BY (`gram_project_id`, `canonical_server_url`) SETTINGS index_granularity = 8192 COMMENT 'Project-scoped Shadow MCP inventory URLs and display metadata';
