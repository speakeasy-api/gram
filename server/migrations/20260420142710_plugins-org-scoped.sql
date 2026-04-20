-- atlas:txmode none

-- Deduplicate slugs that would collide under org-only uniqueness.
-- Appends a UUID prefix to duplicates, keeping the oldest per (org, slug).
UPDATE "plugins" p
SET "slug" = p."slug" || '-' || LEFT(p."id"::text, 8)
WHERE p."id" NOT IN (
  SELECT DISTINCT ON (organization_id, slug) id
  FROM "plugins"
  WHERE deleted IS FALSE
  ORDER BY organization_id, slug, created_at ASC
)
AND p.deleted IS FALSE;

-- Drop the old project-scoped unique index concurrently to avoid ACCESS EXCLUSIVE lock.
DROP INDEX CONCURRENTLY IF EXISTS "plugins_organization_id_project_id_slug_key";

-- Modify "plugins" table
ALTER TABLE "plugins" DROP COLUMN "project_id";

-- Create new org-scoped unique index concurrently.
CREATE UNIQUE INDEX CONCURRENTLY "plugins_organization_id_slug_key" ON "plugins" ("organization_id", "slug") WHERE (deleted IS FALSE);
