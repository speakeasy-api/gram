-- atlas:txmode none

-- Modify "skill_observations" table
ALTER TABLE "skill_observations" ADD COLUMN "efficacy_enqueued_at" timestamptz NULL;
-- Create index "skill_observations_pending_efficacy_idx" to table: "skill_observations"
CREATE INDEX CONCURRENTLY "skill_observations_pending_efficacy_idx" ON "skill_observations" ("project_id", "seen_at", "id") WHERE ((reconciled_at IS NOT NULL) AND (efficacy_enqueued_at IS NULL) AND (session_id IS NOT NULL) AND (skill_version_id IS NOT NULL));
-- Create "skill_efficacy_evaluations" table
CREATE TABLE "skill_efficacy_evaluations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "surface" text NOT NULL,
  "session_id" text NOT NULL,
  "chat_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "skill_version_id" uuid NOT NULL,
  "canonical_sha256" text NOT NULL,
  "observed_at" timestamptz NOT NULL,
  "state" text NOT NULL DEFAULT 'pending',
  "reserved_on" date NULL,
  "attempts" integer NOT NULL DEFAULT 0,
  "last_error" text NULL,
  "scored_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_efficacy_evaluations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_efficacy_evaluations_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_efficacy_evaluations_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "skill_efficacy_evaluations_skill_id_skill_version_id_fkey" FOREIGN KEY ("skill_id", "skill_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Create index "skill_efficacy_evaluations_org_spend_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX "skill_efficacy_evaluations_org_spend_idx" ON "skill_efficacy_evaluations" ("organization_id", "reserved_on") WHERE (state = ANY (ARRAY['reserved'::text, 'scored'::text]));
-- Create index "skill_efficacy_evaluations_organization_id_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX "skill_efficacy_evaluations_organization_id_idx" ON "skill_efficacy_evaluations" ("organization_id");
-- Create index "skill_efficacy_evaluations_pending_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX "skill_efficacy_evaluations_pending_idx" ON "skill_efficacy_evaluations" ("project_id", "observed_at" DESC, "id" DESC) WHERE (state = 'pending'::text);
-- Create index "skill_efficacy_evaluations_scoring_unit_key" to table: "skill_efficacy_evaluations"
CREATE UNIQUE INDEX "skill_efficacy_evaluations_scoring_unit_key" ON "skill_efficacy_evaluations" ("project_id", "session_id", "skill_version_id", "surface");
-- Create index "skill_efficacy_evaluations_skill_spend_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX "skill_efficacy_evaluations_skill_spend_idx" ON "skill_efficacy_evaluations" ("skill_id", "reserved_on") WHERE (state = ANY (ARRAY['reserved'::text, 'scored'::text]));
-- Create index "skill_efficacy_evaluations_stale_reserved_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX "skill_efficacy_evaluations_stale_reserved_idx" ON "skill_efficacy_evaluations" ("updated_at") WHERE (state = 'reserved'::text);
-- Create index "skill_efficacy_evaluations_version_lifetime_spend_idx" to table: "skill_efficacy_evaluations"
CREATE INDEX "skill_efficacy_evaluations_version_lifetime_spend_idx" ON "skill_efficacy_evaluations" ("skill_version_id") WHERE (state = ANY (ARRAY['reserved'::text, 'scored'::text]));
-- Create "skill_efficacy_settings" table
CREATE TABLE "skill_efficacy_settings" (
  "organization_id" text NOT NULL,
  "enabled" boolean NOT NULL,
  "per_skill_daily_cap" integer NOT NULL,
  "org_daily_cap" integer NOT NULL,
  "new_version_burst" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "skill_efficacy_settings_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_efficacy_settings_caps_check" CHECK ((per_skill_daily_cap >= 0) AND (org_daily_cap >= 0) AND (new_version_burst >= 0))
);
