-- atlas:txmode none

-- Drop index "tool_variations_scoped_src_tool_name_key" from table: "tool_variations"
DROP INDEX CONCURRENTLY "tool_variations_scoped_src_tool_name_key";
-- Modify "tool_variations" table
ALTER TABLE "tool_variations" ADD COLUMN "predecessor_id" uuid NULL, ADD COLUMN "src_tool_urn" text NULL, ADD CONSTRAINT "tool_variations_predecessor_id_fkey" FOREIGN KEY ("predecessor_id") REFERENCES "tool_variations" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Modify "tool_variations_groups" table
ALTER TABLE "tool_variations_groups" ADD COLUMN "version" bigint NOT NULL DEFAULT 1;
-- Modify "toolset_versions" table
ALTER TABLE "toolset_versions" ADD COLUMN "tool_variations" uuid[] NOT NULL DEFAULT ARRAY[]::uuid[];
