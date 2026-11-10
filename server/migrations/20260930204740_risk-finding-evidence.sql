-- Create "risk_finding_evidence" table
CREATE TABLE "risk_finding_evidence" (
  "finding_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "match_encrypted" text NOT NULL,
  "created_at" timestamptz NOT NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "expires_at" timestamptz NOT NULL,
  PRIMARY KEY ("organization_id", "project_id", "finding_id"),
  CONSTRAINT "risk_finding_evidence_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "risk_finding_evidence_expires_at_idx" to table: "risk_finding_evidence"
CREATE INDEX "risk_finding_evidence_expires_at_idx" ON "risk_finding_evidence" ("expires_at", "organization_id", "project_id", "finding_id");
