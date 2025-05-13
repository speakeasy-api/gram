-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ADD COLUMN "confirm" text NULL, ADD COLUMN "confirm_prompt" text NULL, ADD COLUMN "x_gram" boolean NULL, ADD COLUMN "original_name" text NULL, ADD COLUMN "original_summary" text NULL, ADD COLUMN "original_description" text NULL;
