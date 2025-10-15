-- atlas:txmode none

-- Modify "tool_variations" table
ALTER TABLE "tool_variations" ADD COLUMN "src_tool_urn" text NULL;
-- Create index "tool_variations_scoped_src_tool_urn_key" to table: "tool_variations"
CREATE UNIQUE INDEX CONCURRENTLY "tool_variations_scoped_src_tool_urn_key" ON "tool_variations" ("group_id", "src_tool_urn") WHERE ((src_tool_urn IS NOT NULL) AND (deleted IS FALSE));
