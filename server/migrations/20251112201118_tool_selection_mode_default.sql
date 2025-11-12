-- Modify "toolsets" table
-- Step 1: Set the default value
ALTER TABLE "toolsets" ALTER COLUMN "tool_selection_mode" SET DEFAULT 'static';

-- Step 2: Update existing NULL values
UPDATE "toolsets" SET "tool_selection_mode" = 'static' WHERE "tool_selection_mode" IS NULL;

-- Step 3: Add NOT NULL constraint
ALTER TABLE "toolsets" ALTER COLUMN "tool_selection_mode" SET NOT NULL;