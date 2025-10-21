-- Modify "prompt_templates" table
ALTER TABLE "prompt_templates" ADD COLUMN "tool_urns_hint" text[] NULL DEFAULT ARRAY[]::text[];
