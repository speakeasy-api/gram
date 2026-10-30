-- Create "memory_digests" table
CREATE TABLE "memory_digests" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "level" text NOT NULL,
  "period_start" date NOT NULL,
  "period_end" date NOT NULL,
  "content" text NOT NULL,
  "chat_ids" uuid[] NOT NULL DEFAULT '{}',
  "memo_ids" uuid[] NOT NULL DEFAULT '{}',
  "child_digest_ids" uuid[] NOT NULL DEFAULT '{}',
  "model" text NULL,
  "input_tokens" integer NULL,
  "output_tokens" integer NULL,
  "search_vector" tsvector NOT NULL GENERATED ALWAYS AS (to_tsvector('english'::regconfig, content)) STORED,
  "generated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "memory_digests_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "memory_digests_level_check" CHECK (level = ANY (ARRAY['day'::text, 'week'::text, 'month'::text])),
  CONSTRAINT "memory_digests_period_check" CHECK (((level = 'day'::text) AND (period_end = (period_start + 1))) OR ((level = 'week'::text) AND (EXTRACT(isodow FROM period_start) = (1)::numeric) AND (period_end = (period_start + 7))) OR ((level = 'month'::text) AND (EXTRACT(day FROM period_start) = (1)::numeric) AND (period_end = ((period_start + '1 mon'::interval))::date))),
  CONSTRAINT "memory_digests_tokens_check" CHECK (((input_tokens IS NULL) OR (input_tokens >= 0)) AND ((output_tokens IS NULL) OR (output_tokens >= 0)))
);
-- Create index "memory_digests_chat_ids_idx" to table: "memory_digests"
CREATE INDEX "memory_digests_chat_ids_idx" ON "memory_digests" USING GIN ("chat_ids");
-- Create index "memory_digests_project_id_level_period_start_key" to table: "memory_digests"
CREATE UNIQUE INDEX "memory_digests_project_id_level_period_start_key" ON "memory_digests" ("project_id", "level", "period_start");
-- Create index "memory_digests_search_vector_idx" to table: "memory_digests"
CREATE INDEX "memory_digests_search_vector_idx" ON "memory_digests" USING GIN ("search_vector");
-- Set comment to column: "id" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."project_id" IS '@access: confidential';
-- Set comment to column: "level" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."level" IS '@access: confidential';
-- Set comment to column: "period_start" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."period_start" IS '@access: confidential';
-- Set comment to column: "period_end" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."period_end" IS '@access: confidential';
-- Set comment to column: "content" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."content" IS 'Model-written digest built only from redacted summaries, memos and child digests.
@access: opaque-restricted';
-- Set comment to column: "chat_ids" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."chat_ids" IS '@access: confidential';
-- Set comment to column: "memo_ids" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."memo_ids" IS '@access: confidential';
-- Set comment to column: "child_digest_ids" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."child_digest_ids" IS '@access: confidential';
-- Set comment to column: "model" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."model" IS '@access: confidential';
-- Set comment to column: "input_tokens" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."input_tokens" IS '@access: confidential';
-- Set comment to column: "output_tokens" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."output_tokens" IS '@access: confidential';
-- Set comment to column: "search_vector" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."search_vector" IS 'Full-text index of content.
@access: opaque-restricted';
-- Set comment to column: "generated_at" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."generated_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "memory_digests"
COMMENT ON COLUMN "memory_digests"."updated_at" IS '@access: confidential';
-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "summary_redacted" boolean NOT NULL DEFAULT false, ADD COLUMN "memory_excluded_at" timestamptz NULL;
-- Set comment to column: "summary_redacted" on table: "chats"
COMMENT ON COLUMN "chats"."summary_redacted" IS '@access: confidential';
-- Set comment to column: "memory_excluded_at" on table: "chats"
COMMENT ON COLUMN "chats"."memory_excluded_at" IS '@access: confidential';
-- Create "memory_memos" table
CREATE TABLE "memory_memos" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "chat_id" uuid NULL,
  "author_user_id" text NULL,
  "author_agent_id" uuid NULL,
  "content" text NOT NULL,
  "search_vector" tsvector NOT NULL GENERATED ALWAYS AS (to_tsvector('english'::regconfig, content)) STORED,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "memory_memos_author_agent_id_fkey" FOREIGN KEY ("author_agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "memory_memos_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "memory_memos_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "memory_memos_author_check" CHECK (num_nonnulls(author_user_id, author_agent_id) <= 1),
  CONSTRAINT "memory_memos_content_check" CHECK ((content <> ''::text) AND (char_length(content) <= 4000))
);
-- Create index "memory_memos_chat_id_idx" to table: "memory_memos"
CREATE INDEX "memory_memos_chat_id_idx" ON "memory_memos" ("chat_id") WHERE (chat_id IS NOT NULL);
-- Create index "memory_memos_project_id_created_at_idx" to table: "memory_memos"
CREATE INDEX "memory_memos_project_id_created_at_idx" ON "memory_memos" ("project_id", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "memory_memos_search_vector_idx" to table: "memory_memos"
CREATE INDEX "memory_memos_search_vector_idx" ON "memory_memos" USING GIN ("search_vector") WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."project_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."chat_id" IS '@access: confidential';
-- Set comment to column: "author_user_id" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."author_user_id" IS '@access: confidential';
-- Set comment to column: "author_agent_id" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."author_agent_id" IS '@access: confidential';
-- Set comment to column: "content" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."content" IS 'Risk-redacted fact text supplied by an agent.
@access: opaque-restricted';
-- Set comment to column: "search_vector" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."search_vector" IS 'Full-text index of content.
@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "memory_memos"
COMMENT ON COLUMN "memory_memos"."deleted" IS '@access: confidential';
