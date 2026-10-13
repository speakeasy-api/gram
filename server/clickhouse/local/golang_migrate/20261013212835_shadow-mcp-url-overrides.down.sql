-- reverse: create "shadow_mcp_inventory_url_overrides_legacy_mv" view
DROP VIEW IF EXISTS `gram`.`shadow_mcp_inventory_url_overrides_legacy_mv`;
-- reverse: create "shadow_mcp_inventory_url_overrides" table
DROP TABLE IF EXISTS `gram`.`shadow_mcp_inventory_url_overrides`;
ALTER TABLE `gram`.`shadow_mcp_inventory_urls` DROP COLUMN IF EXISTS `legacy_override`;
