-- Catalog rows are global reference data, not tenant-owned records.
-- name: InsertProduct :exec
INSERT INTO ai_products (
  id, vendor_key, display_name, category, bundle_ids, binaries, config_dirs,
  process_names, version_plist_key, client_info_names
) VALUES (
  @id, @vendor_key, @display_name, sqlc.narg('category'), @bundle_ids::text[],
  @binaries::text[], @config_dirs::text[], @process_names::text[],
  sqlc.narg('version_plist_key'), @client_info_names::text[]
)
ON CONFLICT DO NOTHING;

-- Stable IDs preserve seed-owned rows even after their URL changes. Historical
-- URL rows also prevent restoring a document that staff deliberately deleted.
-- name: InsertDocument :exec
INSERT INTO ai_product_cimd_documents (
  id, ai_product_id, url, display_name, enabled, is_pattern
)
SELECT @id::uuid, p.id, @url::text, @display_name, @enabled, @is_pattern
FROM ai_products p
WHERE p.id = @ai_product_id AND p.deleted IS FALSE
  AND NOT EXISTS (SELECT 1 FROM ai_product_cimd_documents d WHERE d.url = @url::text)
ON CONFLICT DO NOTHING;

-- Only decision-only rows for the frozen scan library are backfilled. Never
-- overwrite a reference or turn an organization's own definition into a built-in.
-- This deployment-wide repair intentionally spans organizations.
-- name: BackfillDecisions :exec
UPDATE ai_scan_targets t
SET ai_product_id = p.id
FROM ai_products p
WHERE t.id = p.id AND t.id = ANY(@ids::text[]) AND t.ai_product_id IS NULL
  AND t.display_name IS NULL AND t.category IS NULL AND t.version_plist_key IS NULL
  AND cardinality(t.bundle_ids) = 0 AND cardinality(t.binaries) = 0
  AND cardinality(t.config_dirs) = 0 AND cardinality(t.process_names) = 0
  AND cardinality(t.cimd_vendor_keys) = 0 AND cardinality(t.oauth_client_ids) = 0
  AND cardinality(t.client_info_names) = 0;

-- name: ListProducts :many
SELECT * FROM ai_products ORDER BY id;

-- name: ListDocuments :many
SELECT * FROM ai_product_cimd_documents ORDER BY id;

-- name: EditProductFixture :exec
UPDATE ai_products
SET display_name = @display_name, deleted_at = sqlc.narg('deleted_at')
WHERE id = @id;

-- name: EditDocumentFixture :exec
UPDATE ai_product_cimd_documents
SET url = @url, display_name = @display_name, enabled = @enabled,
    deleted_at = sqlc.narg('deleted_at')
WHERE id = @id;
