-- Modify "prompt_templates" table
ALTER TABLE "prompt_templates" DROP CONSTRAINT "prompt_templates_description_check", ADD CONSTRAINT "prompt_templates_description_check" CHECK ((description <> ''::text) AND (char_length(description) <= 500));
