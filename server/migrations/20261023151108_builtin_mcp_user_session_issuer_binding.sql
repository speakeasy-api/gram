-- Modify "builtin_mcp_settings" table
ALTER TABLE "builtin_mcp_settings" ADD COLUMN "user_session_issuer_id" uuid NULL, ADD COLUMN "user_session_issuer_bound" boolean NOT NULL DEFAULT false, ADD CONSTRAINT "builtin_mcp_settings_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Set comment to column: "user_session_issuer_id" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_bound" on table: "builtin_mcp_settings"
COMMENT ON COLUMN "builtin_mcp_settings"."user_session_issuer_bound" IS 'Records an explicit issuer binding even after issuer deletion, preventing implicit fallback.
@access: confidential';
