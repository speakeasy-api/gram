-- Create "risk_execution_evidence" table
CREATE TABLE "risk_execution_evidence" (
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "execution_id" text NOT NULL,
  "phase" text NOT NULL,
  "payload_encrypted" text NOT NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "expires_at" timestamptz NOT NULL,
  PRIMARY KEY ("organization_id", "project_id", "execution_id", "phase"),
  CONSTRAINT "risk_execution_evidence_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "risk_execution_evidence_expires_at_idx" to table: "risk_execution_evidence"
CREATE INDEX "risk_execution_evidence_expires_at_idx" ON "risk_execution_evidence" ("expires_at", "organization_id", "project_id", "execution_id", "phase");
