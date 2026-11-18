-- reverse: create "shadow_mcp_inventory_url_overrides_legacy_mv" view
DROP VIEW `gram`.`shadow_mcp_inventory_url_overrides_legacy_mv`;
-- reverse: create "shadow_mcp_inventory_url_overrides" table
DROP TABLE `gram`.`shadow_mcp_inventory_url_overrides`;
ALTER TABLE `gram`.`shadow_mcp_inventory_urls` DROP COLUMN `legacy_override`;
