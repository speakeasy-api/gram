-- reverse: create "shadow_mcp_inventory_url_overrides" table
DROP TABLE IF EXISTS `gram`.`shadow_mcp_inventory_url_overrides`;
ALTER TABLE `gram`.`shadow_mcp_inventory_urls` DROP COLUMN IF EXISTS `legacy_override`;
