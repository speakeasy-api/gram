-- Create "risk_policy_targets" table
CREATE TABLE "risk_policy_targets" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "risk_policy_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "target_type" text NOT NULL,
  "target_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_policy_targets_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_targets_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_targets_type_check" CHECK (target_type IN ('user', 'role'))
);
-- Create index "risk_policy_targets_policy_target_key" to table: "risk_policy_targets"
CREATE UNIQUE INDEX "risk_policy_targets_policy_target_key" ON "risk_policy_targets" ("risk_policy_id", "target_type", "target_id");
-- Create index "risk_policy_targets_org_target_idx" to table: "risk_policy_targets"
CREATE INDEX "risk_policy_targets_org_target_idx" ON "risk_policy_targets" ("organization_id", "target_type", "target_id");
-- Create index "risk_policy_targets_policy_idx" to table: "risk_policy_targets"
CREATE INDEX "risk_policy_targets_policy_idx" ON "risk_policy_targets" ("risk_policy_id");
