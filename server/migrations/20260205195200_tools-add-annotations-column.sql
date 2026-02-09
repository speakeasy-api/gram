-- Add tool behavior hint columns to http_tool_definitions
ALTER TABLE "http_tool_definitions" ADD COLUMN "read_only_hint" boolean DEFAULT false;
ALTER TABLE "http_tool_definitions" ADD COLUMN "destructive_hint" boolean DEFAULT true;
ALTER TABLE "http_tool_definitions" ADD COLUMN "idempotent_hint" boolean DEFAULT false;
ALTER TABLE "http_tool_definitions" ADD COLUMN "open_world_hint" boolean DEFAULT true;

-- Add tool behavior hint columns to function_tool_definitions
ALTER TABLE "function_tool_definitions" ADD COLUMN "read_only_hint" boolean DEFAULT false;
ALTER TABLE "function_tool_definitions" ADD COLUMN "destructive_hint" boolean DEFAULT true;
ALTER TABLE "function_tool_definitions" ADD COLUMN "idempotent_hint" boolean DEFAULT false;
ALTER TABLE "function_tool_definitions" ADD COLUMN "open_world_hint" boolean DEFAULT true;

-- Add tool behavior hint override columns to tool_variations (NULL = inherit from base tool)
ALTER TABLE "tool_variations" ADD COLUMN "title" text;
ALTER TABLE "tool_variations" ADD COLUMN "read_only_hint" boolean;
ALTER TABLE "tool_variations" ADD COLUMN "destructive_hint" boolean;
ALTER TABLE "tool_variations" ADD COLUMN "idempotent_hint" boolean;
ALTER TABLE "tool_variations" ADD COLUMN "open_world_hint" boolean;
