-- Modify "toolsets" table
ALTER TABLE "toolsets" DROP COLUMN "readonly_mode", ALTER COLUMN "iteration_mode" SET DEFAULT true, DROP COLUMN "published_toolset_version_id";

-- Move all existing toolsets to staging mode (iteration_mode = true)
UPDATE "toolsets" SET iteration_mode = true WHERE iteration_mode = false;
