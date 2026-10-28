-- Create "mcp_skill_links" table
CREATE TABLE "mcp_skill_links" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "skill_id" uuid NOT NULL,
  "pinned_version_id" uuid NULL,
  "target_kind" text NOT NULL,
  "mcp_server_id" uuid NULL,
  "meta_mcp_server_id" uuid NULL,
  "created_by_user_id" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_skill_links_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_skill_links_project_id_mcp_server_id_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_skill_links_project_id_meta_mcp_server_id_fkey" FOREIGN KEY ("project_id", "meta_mcp_server_id") REFERENCES "meta_mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_skill_links_project_id_skill_id_fkey" FOREIGN KEY ("project_id", "skill_id") REFERENCES "skills" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_skill_links_skill_id_pinned_version_id_fkey" FOREIGN KEY ("skill_id", "pinned_version_id") REFERENCES "skill_versions" ("skill_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "mcp_skill_links_target_kind_check" CHECK (target_kind <> ''::text)
);
-- Create index "mcp_skill_links_mcp_server_id_idx" to table: "mcp_skill_links"
CREATE INDEX "mcp_skill_links_mcp_server_id_idx" ON "mcp_skill_links" ("mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "mcp_skill_links_meta_mcp_server_id_idx" to table: "mcp_skill_links"
CREATE INDEX "mcp_skill_links_meta_mcp_server_id_idx" ON "mcp_skill_links" ("meta_mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "mcp_skill_links_organization_id_target_kind_idx" to table: "mcp_skill_links"
CREATE INDEX "mcp_skill_links_organization_id_target_kind_idx" ON "mcp_skill_links" ("organization_id", "target_kind") WHERE (deleted IS FALSE);
-- Create index "mcp_skill_links_project_id_skill_id_target_key" to table: "mcp_skill_links"
CREATE UNIQUE INDEX "mcp_skill_links_project_id_skill_id_target_key" ON "mcp_skill_links" ("project_id", "skill_id", "target_kind", "mcp_server_id", "meta_mcp_server_id") NULLS NOT DISTINCT WHERE (deleted IS FALSE);
-- Create index "mcp_skill_links_skill_id_pinned_version_id_idx" to table: "mcp_skill_links"
CREATE INDEX "mcp_skill_links_skill_id_pinned_version_id_idx" ON "mcp_skill_links" ("skill_id", "pinned_version_id");
-- Set comment to column: "id" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."project_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."skill_id" IS '@access: confidential';
-- Set comment to column: "pinned_version_id" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."pinned_version_id" IS '@access: confidential';
-- Set comment to column: "target_kind" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."target_kind" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "meta_mcp_server_id" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."meta_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_skill_links"
COMMENT ON COLUMN "mcp_skill_links"."deleted" IS '@access: confidential';
