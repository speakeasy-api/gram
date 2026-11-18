-- Modify "ai_integration_configs" table
ALTER TABLE "ai_integration_configs" ADD COLUMN "settings" jsonb NOT NULL DEFAULT '{}';
-- Set comment to column: "settings" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."settings" IS 'Provider settings JSON; holds admin emails and customer directory structure.
@access: opaque-restricted';
