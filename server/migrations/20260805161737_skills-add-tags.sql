-- atlas:txmode none

-- Modify "skills" table
ALTER TABLE "skills" ADD CONSTRAINT "skills_tags_check" CHECK (array_length(tags, 1) <= 40), ADD COLUMN "tags" text[] NOT NULL DEFAULT ARRAY[]::text[];
-- Create index "skills_tags_gin" to table: "skills"
CREATE INDEX CONCURRENTLY "skills_tags_gin" ON "skills" USING gin ("tags") WHERE (archived_at IS NULL);
