-- Modify "tool_variations" table
ALTER TABLE "tool_variations" ADD COLUMN "predecessor_id" uuid NULL, ADD COLUMN "src_tool_urn" text NULL, ADD CONSTRAINT "tool_variations_predecessor_id_fkey" FOREIGN KEY ("predecessor_id") REFERENCES "tool_variations" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Modify "toolset_versions" table
ALTER TABLE "toolset_versions" ADD COLUMN "tool_variations" uuid[] NOT NULL DEFAULT ARRAY[]::uuid[];
