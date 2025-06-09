-- atlas:txmode none

-- Modify "toolset_prompts" table
ALTER TABLE "toolset_prompts" DROP CONSTRAINT "toolset_prompts_prompt_history_id_fkey", ADD CONSTRAINT "toolset_prompts_prompt_name_check" CHECK ((prompt_name <> ''::text) AND (char_length(prompt_name) <= 40)), ADD COLUMN "prompt_name" text NOT NULL;
-- Create index "toolset_prompts_toolset_id_prompt_name_key" to table: "toolset_prompts"
CREATE UNIQUE INDEX CONCURRENTLY "toolset_prompts_toolset_id_prompt_name_key" ON "toolset_prompts" ("toolset_id", "prompt_name");
-- Modify "prompt_templates" table
ALTER TABLE "prompt_templates" DROP CONSTRAINT "prompt_templates_project_id_history_id_key";
-- Drop index "toolset_prompts_project_id_history_id_key" from table: "toolset_prompts"
DROP INDEX CONCURRENTLY "toolset_prompts_project_id_history_id_key";
