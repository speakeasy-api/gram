-- Catalog products and documents are global reference data.
-- Byte-order sorting by id, then url, makes database snapshots deterministic.
-- name: ListCatalogProducts :many
SELECT id, vendor_key, display_name, category, bundle_ids, binaries, config_dirs,
  process_names, version_plist_key, client_info_names
FROM ai_products
WHERE deleted IS FALSE
ORDER BY id COLLATE "C";

-- name: ListCatalogDocuments :many
SELECT d.id, d.ai_product_id, d.url, d.display_name, d.enabled, d.is_pattern
FROM ai_product_cimd_documents d
JOIN ai_products p ON p.id = d.ai_product_id
WHERE d.deleted IS FALSE AND p.deleted IS FALSE
ORDER BY d.url COLLATE "C";
