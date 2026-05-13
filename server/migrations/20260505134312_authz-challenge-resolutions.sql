-- Create "authz_challenge_resolutions" table
CREATE TABLE "authz_challenge_resolutions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "challenge_id" text NOT NULL,
  "principal_urn" text NOT NULL,
  "scope" text NOT NULL,
  "resource_kind" text NOT NULL DEFAULT '',
  "resource_id" text NOT NULL DEFAULT '',
  "resolution_type" text NOT NULL,
  "role_slug" text NULL,
  "resolved_by" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "authz_challenge_resolutions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "authz_challenge_resolutions_org_challenge_key" to table: "authz_challenge_resolutions"
CREATE UNIQUE INDEX "authz_challenge_resolutions_org_challenge_key" ON "authz_challenge_resolutions" ("organization_id", "challenge_id");
-- Create index "authz_challenge_resolutions_org_principal_idx" to table: "authz_challenge_resolutions"
CREATE INDEX "authz_challenge_resolutions_org_principal_idx" ON "authz_challenge_resolutions" ("organization_id", "principal_urn");
-- Set comment to table: "authz_challenge_resolutions"
COMMENT ON TABLE "authz_challenge_resolutions" IS 'Tracks admin resolutions of authz challenge denials. challenge_id references authz_challenges.id in ClickHouse (soft cross-DB reference).';
-- Set comment to column: "challenge_id" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."challenge_id" IS 'UUID of the denied challenge in the ClickHouse authz_challenges table.';
-- Set comment to column: "principal_urn" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."principal_urn" IS 'The principal that was denied, copied from the challenge for query convenience.';
-- Set comment to column: "resolution_type" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."resolution_type" IS 'How the challenge was resolved: role_assigned, dismissed.';
-- Set comment to column: "role_slug" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."role_slug" IS 'When resolution_type=role_assigned, the role slug that was assigned to the principal.';
-- Set comment to column: "resolved_by" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."resolved_by" IS 'URN of the admin who resolved the challenge.';
