-- atlas:txmode none

-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD CONSTRAINT "toolsets_mcp_slug_check" CHECK ((mcp_slug IS NULL) OR ((mcp_slug <> ''::text) AND (char_length(mcp_slug) <= 40))), ADD COLUMN "mcp_slug" text NULL, ADD COLUMN "mcp_is_public" boolean NOT NULL DEFAULT false;
-- Create index "toolsets_mcp_slug_key" to table: "toolsets"
CREATE UNIQUE INDEX CONCURRENTLY "toolsets_mcp_slug_key" ON "toolsets" ("mcp_slug") WHERE ((mcp_slug IS NOT NULL) AND (deleted IS FALSE));
