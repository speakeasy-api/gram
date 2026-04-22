-- atlas:txmode none

ALTER TABLE "principal_grants"
  ADD COLUMN "selectors" jsonb NULL,
  ADD CONSTRAINT "principal_grants_selectors_check" CHECK ("selectors" IS NULL OR jsonb_typeof("selectors") = 'object') NOT VALID;

ALTER TABLE "principal_grants"
  VALIDATE CONSTRAINT "principal_grants_selectors_check";

-- Set comment to table: "principal_grants"
COMMENT ON TABLE "principal_grants" IS 'RBAC grants. Normalized: one row per (org, principal, scope, resource). Resource=''*'' means unrestricted. Selectors can further constrain applicability.';

-- Set comment to column: "selectors" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."selectors" IS 'Optional JSON selector constraints refining when the grant applies. NULL means the grant has no selector constraints.';

CREATE INDEX CONCURRENTLY "principal_grants_selectors_idx"
  ON "principal_grants"
  USING GIN ("selectors")
  WHERE "selectors" IS NOT NULL;
