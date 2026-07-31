-- Create "skill_share_links" table
CREATE TABLE "skill_share_links" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "token" text NOT NULL,
  "created_by_user_id" text NOT NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "skill_share_links_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "skill_share_links_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "skill_share_links_project_id_idx" to table: "skill_share_links"
CREATE INDEX "skill_share_links_project_id_idx" ON "skill_share_links" ("project_id");
-- Create index "skill_share_links_skill_id_key" to table: "skill_share_links"
CREATE UNIQUE INDEX "skill_share_links_skill_id_key" ON "skill_share_links" ("skill_id") WHERE (revoked_at IS NULL);
-- Create index "skill_share_links_token_key" to table: "skill_share_links"
CREATE UNIQUE INDEX "skill_share_links_token_key" ON "skill_share_links" ("token");
