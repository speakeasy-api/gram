-- Create "organization_onboarding" table
CREATE TABLE "organization_onboarding" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "preset" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_onboarding_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "organization_onboarding_organization_id_key" to table: "organization_onboarding"
CREATE UNIQUE INDEX "organization_onboarding_organization_id_key" ON "organization_onboarding" ("organization_id");
