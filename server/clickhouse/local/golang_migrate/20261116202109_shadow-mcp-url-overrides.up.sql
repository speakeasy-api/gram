ALTER TABLE `gram`.`shadow_mcp_inventory_urls` ADD COLUMN `legacy_override` UInt8 DEFAULT 1 COMMENT '1 when written before overrides moved to shadow_mcp_inventory_url_overrides, so server_name_override is meaningful. Current writers set 0.
@access: confidential';
-- create "shadow_mcp_inventory_url_overrides" table
CREATE TABLE `gram`.`shadow_mcp_inventory_url_overrides` (
  `gram_project_id` UUID COMMENT '@access: confidential',
  `canonical_server_url` String COMMENT '@access: opaque-restricted',
  `server_name_override` String COMMENT 'Admin-set display name. Empty means the override was cleared.
@access: confidential',
  `updated_at` DateTime64(9, 'UTC') COMMENT '@access: confidential'
) ENGINE = ReplacingMergeTree(updated_at)
PRIMARY KEY (`gram_project_id`, `canonical_server_url`) ORDER BY (`gram_project_id`, `canonical_server_url`) SETTINGS index_granularity = 8192 COMMENT 'Admin-set Shadow MCP display names, kept apart from observation rows so ingest writes cannot overwrite them';
-- create "shadow_mcp_inventory_url_overrides_legacy_mv" view
CREATE MATERIALIZED VIEW `gram`.`shadow_mcp_inventory_url_overrides_legacy_mv` TO `gram`.`shadow_mcp_inventory_url_overrides` AS SELECT gram_project_id, canonical_server_url, server_name_override, updated_at FROM gram.shadow_mcp_inventory_urls WHERE legacy_override = 1;
