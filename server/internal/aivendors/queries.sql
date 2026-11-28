-- Catalog products and documents are global reference data.
-- Byte-order sorting by id, then url, makes database snapshots deterministic.
-- name: ListCatalogProducts :many
-- NULL array elements are dropped and nested arrays flattened so one bad row cannot fail the read.
SELECT id, vendor_key, display_name, category,
  ARRAY(SELECT e FROM unnest(bundle_ids) AS e WHERE e IS NOT NULL)::text[] AS bundle_ids,
  ARRAY(SELECT e FROM unnest(binaries) AS e WHERE e IS NOT NULL)::text[] AS binaries,
  ARRAY(SELECT e FROM unnest(config_dirs) AS e WHERE e IS NOT NULL)::text[] AS config_dirs,
  ARRAY(SELECT e FROM unnest(process_names) AS e WHERE e IS NOT NULL)::text[] AS process_names,
  version_plist_key,
  ARRAY(SELECT e FROM unnest(client_info_names) AS e WHERE e IS NOT NULL)::text[] AS client_info_names
FROM ai_products
WHERE deleted IS FALSE
ORDER BY id COLLATE "C";

-- name: ListCatalogDocuments :many
SELECT d.id, d.ai_product_id, d.url, d.display_name, d.enabled, d.is_pattern
FROM ai_product_cimd_documents d
JOIN ai_products p ON p.id = d.ai_product_id
WHERE d.deleted IS FALSE AND p.deleted IS FALSE
ORDER BY d.url COLLATE "C";

-- Documents that no longer identify a live client: soft-deleted documents and
-- every document of a soft-deleted product, most recently removed first. Read
-- only to keep blocks applying. A URL a live document of a live product holds
-- now is left out, so it can never be credited to its old product.
-- name: ListRemovedCatalogDocuments :many
SELECT d.url, d.is_pattern, d.ai_product_id, p.vendor_key, p.category, p.display_name AS product_display_name
FROM ai_product_cimd_documents d
JOIN ai_products p ON p.id = d.ai_product_id
WHERE (d.deleted IS TRUE OR p.deleted IS TRUE)
  AND NOT EXISTS (
    SELECT 1 FROM ai_product_cimd_documents l
    JOIN ai_products lp ON lp.id = l.ai_product_id
    WHERE l.url = d.url AND l.deleted IS FALSE AND lp.deleted IS FALSE
  )
ORDER BY LEAST(d.deleted_at, p.deleted_at) DESC, d.url COLLATE "C", d.ai_product_id COLLATE "C"
LIMIT @max_rows;

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
