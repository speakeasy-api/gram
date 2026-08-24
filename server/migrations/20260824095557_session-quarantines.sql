-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "session_quarantine_fail_closed" boolean NOT NULL DEFAULT false;
-- Create "session_quarantines" table
CREATE TABLE "session_quarantines" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "session_id" text NOT NULL,
  "risk_policy_id" uuid NULL,
  "risk_policy_name" text NOT NULL,
  "user_id" text NOT NULL DEFAULT '',
  "reason" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "released_at" timestamptz NULL,
  "released_by" text NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "session_quarantines_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "session_quarantines_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "session_quarantines_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "session_quarantines_active_idx" to table: "session_quarantines"
CREATE INDEX "session_quarantines_active_idx" ON "session_quarantines" ("organization_id", "project_id", "created_at" DESC) WHERE (released_at IS NULL);
-- Create index "session_quarantines_active_session_key" to table: "session_quarantines"
CREATE UNIQUE INDEX "session_quarantines_active_session_key" ON "session_quarantines" ("session_id") WHERE (released_at IS NULL);
