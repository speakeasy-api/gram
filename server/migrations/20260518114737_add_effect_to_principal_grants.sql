-- atlas:txmode none

-- Modify "principal_grants" table
ALTER TABLE "principal_grants" ADD CONSTRAINT "principal_grants_effect_check" CHECK ((effect IS NULL) OR (effect = ANY (ARRAY['allow'::text, 'deny'::text]))), ADD COLUMN "effect" text NULL;
-- Create index "principal_grants_org_principal_scope_effect_selector_key" to table: "principal_grants"
CREATE UNIQUE INDEX CONCURRENTLY "principal_grants_org_principal_scope_effect_selector_key" ON "principal_grants" ("organization_id", "principal_urn", "scope", (COALESCE(effect, 'allow'::text)), "selectors");
-- Set comment to column: "effect" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."effect" IS 'Whether this grant allows or denies the scope. NULL = allow for backward compatibility.';
