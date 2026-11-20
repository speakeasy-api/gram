-- Catalog products and documents are global reference data.
-- Byte-order sorting by id, then url, makes database snapshots deterministic.
-- name: ListCatalogProducts :many
-- NULL array elements are dropped so one bad row cannot fail the scan.
SELECT id, vendor_key, display_name, category,
  array_remove(bundle_ids, NULL)::text[] AS bundle_ids,
  array_remove(binaries, NULL)::text[] AS binaries,
  array_remove(config_dirs, NULL)::text[] AS config_dirs,
  array_remove(process_names, NULL)::text[] AS process_names,
  version_plist_key,
  array_remove(client_info_names, NULL)::text[] AS client_info_names
FROM ai_products
WHERE deleted IS FALSE
ORDER BY id COLLATE "C";

-- name: ListCatalogDocuments :many
SELECT d.id, d.ai_product_id, d.url, d.display_name, d.enabled, d.is_pattern
FROM ai_product_cimd_documents d
JOIN ai_products p ON p.id = d.ai_product_id
WHERE d.deleted IS FALSE AND p.deleted IS FALSE
ORDER BY d.url COLLATE "C";

-- Removals the built-in fallback honours when the live catalog is rejected.
-- name: ListDeletedProductIDs :many
SELECT id FROM ai_products WHERE deleted IS TRUE ORDER BY id COLLATE "C";

-- A URL with no enabled live row is removed; live reports whether a disabled live row still holds it.
-- name: ListRemovedDocumentURLs :many
SELECT d.url, COALESCE(bool_or(d.deleted IS FALSE AND p.deleted IS FALSE), FALSE)::boolean AS live
FROM ai_product_cimd_documents d
JOIN ai_products p ON p.id = d.ai_product_id
GROUP BY d.url
HAVING NOT COALESCE(bool_or(d.deleted IS FALSE AND p.deleted IS FALSE AND d.enabled), FALSE)
ORDER BY d.url COLLATE "C";
