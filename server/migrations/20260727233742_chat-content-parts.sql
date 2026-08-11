-- atlas:txmode none

-- Create "chat_content_parts" table
CREATE TABLE "chat_content_parts" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "chat_id" uuid NOT NULL,
  "project_id" uuid NULL,
  "kind" text NOT NULL,
  "content_asset_url" text NOT NULL,
  "external_id" text NULL,
  "parent_chat_message_id" uuid NULL,
  "version" integer NULL,
  "source" text NULL,
  "metadata" jsonb NULL,
  "risk_analyzed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_content_parts_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_content_parts_parent_chat_message_id_fkey" FOREIGN KEY ("parent_chat_message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "chat_content_parts_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "chat_content_parts_chat_id_idx" to table: "chat_content_parts"
CREATE INDEX "chat_content_parts_chat_id_idx" ON "chat_content_parts" ("chat_id");
-- Create index "chat_content_parts_risk_analyzed_at_null_idx" to table: "chat_content_parts"
CREATE INDEX "chat_content_parts_risk_analyzed_at_null_idx" ON "chat_content_parts" ("project_id", "id") WHERE (risk_analyzed_at IS NULL);
-- Modify "risk_results" table
-- Constraints are added NOT VALID and validated separately: risk_results is a
-- large append-heavy table, and a validating ADD CONSTRAINT holds ACCESS
-- EXCLUSIVE for the whole scan. VALIDATE takes only SHARE UPDATE EXCLUSIVE, so
-- the scan runs without blocking reads or writes (atlas lint PG305, PG306).
ALTER TABLE "risk_results" ALTER COLUMN "chat_message_id" DROP NOT NULL, ADD COLUMN "chat_content_part_id" uuid NULL;
ALTER TABLE "risk_results" ADD CONSTRAINT "risk_results_anchor_check" CHECK ((chat_message_id IS NULL) <> (chat_content_part_id IS NULL)) NOT VALID;
ALTER TABLE "risk_results" VALIDATE CONSTRAINT "risk_results_anchor_check";
ALTER TABLE "risk_results" ADD CONSTRAINT "risk_results_chat_content_part_id_fkey" FOREIGN KEY ("chat_content_part_id") REFERENCES "chat_content_parts" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "risk_results" VALIDATE CONSTRAINT "risk_results_chat_content_part_id_fkey";
-- Create index "risk_results_project_chat_content_part_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_project_chat_content_part_idx" ON "risk_results" ("project_id", "chat_content_part_id");
