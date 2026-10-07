-- Create "shadow_mcp_inventory_url_overrides" table
CREATE TABLE `gram`.`shadow_mcp_inventory_url_overrides` (
  `gram_project_id` UUID,
  `canonical_server_url` String,
  `server_name_override` String COMMENT 'Admin-set display name. Empty means the override was cleared.',
  `updated_at` DateTime64(9, 'UTC')
) ENGINE = ReplacingMergeTree(updated_at)
PRIMARY KEY (`gram_project_id`, `canonical_server_url`) ORDER BY (`gram_project_id`, `canonical_server_url`) SETTINGS index_granularity = 8192 COMMENT 'Admin-set Shadow MCP display names, kept apart from observation rows so ingest writes cannot overwrite them';
-- Hand-added: copy each URL's current override into the new table. Aliases
-- avoid the base column names so they are not substituted into argMax.
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
