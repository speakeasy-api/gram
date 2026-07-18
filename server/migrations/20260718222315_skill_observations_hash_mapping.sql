-- Create "skill_observations" table
CREATE TABLE "skill_observations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "idempotency_key" text NULL,
  "provider" text NOT NULL,
  "user_id" text NULL,
  "user_email" text NULL,
  "hostname" text NULL,
  "session_id" text NULL,
  "skill_name" text NOT NULL,
  "source_level" text NULL,
  "source_path" text NULL,
  "raw_sha256" text NULL,
  "seen_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_observations_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "skill_observations_project_id_idempotency_key_key" to table: "skill_observations"
CREATE UNIQUE INDEX "skill_observations_project_id_idempotency_key_key" ON "skill_observations" ("project_id", "idempotency_key") WHERE (idempotency_key IS NOT NULL);
-- Create index "skill_observations_project_id_raw_sha256_idx" to table: "skill_observations"
CREATE INDEX "skill_observations_project_id_raw_sha256_idx" ON "skill_observations" ("project_id", "raw_sha256") WHERE (raw_sha256 IS NOT NULL);
-- Create index "skill_observations_project_id_skill_name_seen_at_id_idx" to table: "skill_observations"
CREATE INDEX "skill_observations_project_id_skill_name_seen_at_id_idx" ON "skill_observations" ("project_id", "skill_name", "seen_at" DESC, "id" DESC);
-- Create "skill_raw_hashes" table
CREATE TABLE "skill_raw_hashes" (
  "project_id" uuid NOT NULL,
  "raw_sha256" text NOT NULL,
  "canonical_sha256" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("project_id", "raw_sha256"),
  CONSTRAINT "skill_raw_hashes_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
