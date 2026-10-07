ALTER TABLE `gram`.`shadow_mcp_inventory_urls` ADD COLUMN IF NOT EXISTS `legacy_override` UInt8 DEFAULT 1 COMMENT '1 when written before overrides moved to shadow_mcp_inventory_url_overrides, so server_name_override is meaningful. Current writers set 0.';
-- create "shadow_mcp_inventory_url_overrides" table
CREATE TABLE IF NOT EXISTS `gram`.`shadow_mcp_inventory_url_overrides` (
  `gram_project_id` UUID,
  `canonical_server_url` String,
  `server_name_override` String COMMENT 'Admin-set display name. Empty means the override was cleared.',
  `updated_at` DateTime64(9, 'UTC')
) ENGINE = ReplacingMergeTree(updated_at)
PRIMARY KEY (`gram_project_id`, `canonical_server_url`) ORDER BY (`gram_project_id`, `canonical_server_url`) SETTINGS index_granularity = 8192 COMMENT 'Admin-set Shadow MCP display names, kept apart from observation rows so ingest writes cannot overwrite them';
-- create "shadow_mcp_inventory_url_overrides_legacy_mv" view
CREATE MATERIALIZED VIEW IF NOT EXISTS `gram`.`shadow_mcp_inventory_url_overrides_legacy_mv` TO `gram`.`shadow_mcp_inventory_url_overrides` AS SELECT gram_project_id, canonical_server_url, server_name_override, updated_at FROM gram.shadow_mcp_inventory_urls WHERE legacy_override = 1;
-- Hand-added: copy names that existed before the view above started mirroring
-- new writes. It runs after the view is created so no write falls in between.
-- Every legacy write carried the current name forward, so the newest row per
-- URL holds both the current name and its version. A re-run only adds
-- duplicate versions, which the ReplacingMergeTree collapses. Aliases avoid
-- the base column names so they are not substituted into argMax.
INSERT INTO `gram`.`shadow_mcp_inventory_url_overrides`
  (`gram_project_id`, `canonical_server_url`, `server_name_override`, `updated_at`)
SELECT
  `gram_project_id`,
  `canonical_server_url`,
  argMax(`server_name_override`, `updated_at`) AS `latest_override`,
  max(`updated_at`) AS `latest_updated_at`
FROM `gram`.`shadow_mcp_inventory_urls`
GROUP BY `gram_project_id`, `canonical_server_url`
HAVING `latest_override` != '';
