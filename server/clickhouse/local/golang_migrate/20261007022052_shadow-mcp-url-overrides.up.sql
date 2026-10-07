ALTER TABLE `gram`.`shadow_mcp_inventory_urls` ADD COLUMN IF NOT EXISTS `legacy_override` UInt8 DEFAULT 1 COMMENT '1 when written before overrides moved to shadow_mcp_inventory_url_overrides, so server_name_override is meaningful. Current writers set 0.';
-- create "shadow_mcp_inventory_url_overrides" table
CREATE TABLE IF NOT EXISTS `gram`.`shadow_mcp_inventory_url_overrides` (
  `gram_project_id` UUID,
  `canonical_server_url` String,
  `server_name_override` String COMMENT 'Admin-set display name. Empty means the override was cleared.',
  `updated_at` DateTime64(9, 'UTC')
) ENGINE = ReplacingMergeTree(updated_at)
PRIMARY KEY (`gram_project_id`, `canonical_server_url`) ORDER BY (`gram_project_id`, `canonical_server_url`) SETTINGS index_granularity = 8192 COMMENT 'Admin-set Shadow MCP display names, kept apart from observation rows so ingest writes cannot overwrite them';
