-- atlas:txmode none

-- Drop index "tool_variations_scoped_src_tool_name_key" from table: "tool_variations"
DROP INDEX CONCURRENTLY "tool_variations_scoped_src_tool_name_key";
-- Drop index "tool_variations_scoped_src_tool_urn_key" from table: "tool_variations"
DROP INDEX CONCURRENTLY "tool_variations_scoped_src_tool_urn_key";
-- Modify "tool_variations" table
ALTER TABLE "tool_variations" ALTER COLUMN "src_tool_urn" SET NOT NULL;
-- Create index "tool_variations_scoped_src_tool_urn_key" to table: "tool_variations"
CREATE UNIQUE INDEX CONCURRENTLY "tool_variations_scoped_src_tool_urn_key" ON "tool_variations" ("group_id", "src_tool_urn") WHERE (deleted IS FALSE);
