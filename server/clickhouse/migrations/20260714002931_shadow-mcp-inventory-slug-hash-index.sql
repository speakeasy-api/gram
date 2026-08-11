ALTER TABLE `shadow_mcp_inventory_urls` ADD INDEX `idx_shadow_mcp_inventory_urls_slug_hash` ((substring(lower(hex(SHA256(canonical_server_url))), 1, 8))) TYPE bloom_filter(0.01) GRANULARITY 1;
ALTER TABLE `shadow_mcp_inventory_urls` MATERIALIZE INDEX `idx_shadow_mcp_inventory_urls_slug_hash`;
