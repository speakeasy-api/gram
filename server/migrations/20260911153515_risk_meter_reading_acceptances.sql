-- Create "risk_meter_reading_acceptances" table
CREATE TABLE "risk_meter_reading_acceptances" (
  "id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "envelope" bytea NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_meter_reading_acceptances_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
