-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "custom_rule_ids" text[] NOT NULL DEFAULT '{}';
-- Create "risk_custom_detection_rules" table
CREATE TABLE "risk_custom_detection_rules" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "rule_id" text NOT NULL,
  "title" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "regex" text NULL,
  "severity" text NOT NULL DEFAULT 'medium',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_custom_detection_rules_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_custom_detection_rules_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "risk_custom_detection_rules_project_rule_id_key" to table: "risk_custom_detection_rules"
CREATE UNIQUE INDEX "risk_custom_detection_rules_project_rule_id_key" ON "risk_custom_detection_rules" ("project_id", "rule_id") WHERE (deleted IS FALSE);
