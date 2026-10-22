-- Catalog products and documents are global reference data.
-- name: ListCatalogProducts :many
SELECT * FROM ai_products WHERE deleted IS FALSE ORDER BY id;

-- name: ListCatalogDocuments :many
SELECT d.* FROM ai_product_cimd_documents d
JOIN ai_products p ON p.id = d.ai_product_id
WHERE d.deleted IS FALSE AND p.deleted IS FALSE
ORDER BY d.url;
