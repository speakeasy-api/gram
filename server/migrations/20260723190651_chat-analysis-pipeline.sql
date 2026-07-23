-- atlas:txmode none

-- Create index "chats_project_id_created_at_id_idx" to table: "chats"
CREATE INDEX CONCURRENTLY "chats_project_id_created_at_id_idx" ON "chats" ("project_id", "created_at", "id");
-- Create "chat_analysis_evaluations" table
CREATE TABLE "chat_analysis_evaluations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "chat_id" uuid NOT NULL,
  "session_id" text NOT NULL DEFAULT '',
  "judge" text NOT NULL,
  "observed_at" timestamptz NOT NULL,
  "state" text NOT NULL DEFAULT 'pending',
  "reserved_on" date NULL,
  "attempts" integer NOT NULL DEFAULT 0,
  "last_error" text NULL,
  "scored_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_analysis_evaluations_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_analysis_evaluations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_analysis_evaluations_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "chat_analysis_evaluations_org_spend_idx" to table: "chat_analysis_evaluations"
CREATE INDEX "chat_analysis_evaluations_org_spend_idx" ON "chat_analysis_evaluations" ("organization_id", "reserved_on") WHERE (reserved_on IS NOT NULL);
-- Create index "chat_analysis_evaluations_pending_idx" to table: "chat_analysis_evaluations"
CREATE INDEX "chat_analysis_evaluations_pending_idx" ON "chat_analysis_evaluations" ("project_id", "observed_at" DESC, "id" DESC) WHERE (state = 'pending'::text);
-- Create index "chat_analysis_evaluations_scoring_unit_key" to table: "chat_analysis_evaluations"
CREATE UNIQUE INDEX "chat_analysis_evaluations_scoring_unit_key" ON "chat_analysis_evaluations" ("project_id", "chat_id", "judge");
-- Create "chat_analysis_settings" table
CREATE TABLE "chat_analysis_settings" (
  "organization_id" text NOT NULL,
  "judge" text NOT NULL,
  "enabled" boolean NOT NULL,
  "daily_cap" integer NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id", "judge"),
  CONSTRAINT "chat_analysis_settings_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_analysis_settings_daily_cap_check" CHECK (daily_cap >= 0)
);
