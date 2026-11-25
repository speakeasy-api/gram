-- A function runner key's organization is its project's organization. Only
-- organizations with an organization_metadata row count, because
-- functions_access.organization_id references that table.

-- name: CountRunnerKeys :one
-- Counts runner keys, soft-deleted ones included, by what the backfill can do
-- with them. Only pending rows are written; the rest are reported. A row
-- whose organization_id is set always matches its project's organization:
-- functions_access_project_tenant_fkey enforces it.
SELECT
  COUNT(*) FILTER (
    WHERE fa.organization_id IS NULL AND om.id IS NOT NULL
  )::bigint AS pending,
  COUNT(*) FILTER (
    WHERE fa.organization_id IS NULL AND om.id IS NULL
  )::bigint AS missing_organization
FROM functions_access AS fa
LEFT JOIN projects AS p ON p.id = fa.project_id
LEFT JOIN organization_metadata AS om ON om.id = p.organization_id;

-- name: FillBatch :one
-- Looks at the next batch_size runner keys without an organization_id, in id
-- order after the given id, and sets each one's organization_id to its
-- project's organization. Rows whose project's organization has no metadata
-- row are skipped and stay NULL. It returns the last id it looked at, so the
-- next batch continues after it, and how many rows it filled. Paging by the
-- primary key keeps each batch to one short index range scan. updated_at is
-- left alone.
WITH batch AS (
  SELECT fa.id
  FROM functions_access AS fa
  WHERE fa.organization_id IS NULL
    AND (sqlc.narg('after')::uuid IS NULL OR fa.id > sqlc.narg('after')::uuid)
  ORDER BY fa.id
  LIMIT @batch_size::int
),
filled AS (
  UPDATE functions_access AS fa
  SET organization_id = p.organization_id
  FROM batch, projects AS p, organization_metadata AS om
  WHERE fa.id = batch.id
    AND fa.organization_id IS NULL
    AND p.id = fa.project_id
    AND om.id = p.organization_id
  RETURNING fa.id
)
SELECT
  (SELECT b.id FROM batch AS b ORDER BY b.id DESC LIMIT 1)::uuid AS last_id,
  (SELECT COUNT(*) FROM filled)::bigint AS filled;
