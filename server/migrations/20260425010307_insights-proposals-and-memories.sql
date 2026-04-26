-- Create "insights_memories" table
CREATE TABLE "insights_memories" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "kind" text NOT NULL,
  "content" text NOT NULL,
  "tags" text[] NOT NULL DEFAULT '{}',
  "source_chat_id" uuid NULL,
  "usefulness_score" integer NOT NULL DEFAULT 0,
  "expires_at" timestamptz NULL,
  "last_used_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "insights_memories_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "insights_memories_source_chat_id_fkey" FOREIGN KEY ("source_chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "insights_memories_content_length_check" CHECK (char_length(content) <= 2000),
  CONSTRAINT "insights_memories_kind_check" CHECK (kind = ANY (ARRAY['fact'::text, 'playbook'::text, 'glossary'::text, 'finding'::text]))
);
-- Create index "insights_memories_project_kind_last_used_idx" to table: "insights_memories"
CREATE INDEX "insights_memories_project_kind_last_used_idx" ON "insights_memories" ("project_id", "kind", "last_used_at" DESC) WHERE (deleted IS FALSE);
-- Create index "insights_memories_tags_gin_idx" to table: "insights_memories"
CREATE INDEX "insights_memories_tags_gin_idx" ON "insights_memories" USING gin ("tags") WHERE (deleted IS FALSE);
-- Create "insights_proposals" table
CREATE TABLE "insights_proposals" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "kind" text NOT NULL,
  "target_ref" text NOT NULL,
  "current_value" jsonb NOT NULL,
  "proposed_value" jsonb NOT NULL,
  "applied_value" jsonb NULL,
  "reasoning" text NULL,
  "source_chat_id" uuid NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "applied_at" timestamptz NULL,
  "dismissed_at" timestamptz NULL,
  "rolled_back_at" timestamptz NULL,
  "applied_by_user_id" text NULL,
  "dismissed_by_user_id" text NULL,
  "rolled_back_by_user_id" text NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "insights_proposals_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "insights_proposals_source_chat_id_fkey" FOREIGN KEY ("source_chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "insights_proposals_kind_check" CHECK (kind = ANY (ARRAY['tool_variation'::text, 'toolset_change'::text])),
  CONSTRAINT "insights_proposals_status_check" CHECK (status = ANY (ARRAY['pending'::text, 'applied'::text, 'dismissed'::text, 'superseded'::text, 'rolled_back'::text]))
);
-- Create index "insights_proposals_project_status_created_idx" to table: "insights_proposals"
CREATE INDEX "insights_proposals_project_status_created_idx" ON "insights_proposals" ("project_id", "status", "created_at" DESC);
-- Create index "insights_proposals_project_target_idx" to table: "insights_proposals"
CREATE INDEX "insights_proposals_project_target_idx" ON "insights_proposals" ("project_id", "kind", "target_ref");
