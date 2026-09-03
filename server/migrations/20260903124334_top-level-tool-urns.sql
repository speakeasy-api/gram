-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "top_level_tool_urns" text[] NOT NULL DEFAULT ARRAY[]::text[];
