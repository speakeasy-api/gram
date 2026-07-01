-- Create "tool_call_blocks" table
CREATE TABLE "tool_call_blocks" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "provider" text NOT NULL,
  "reason" text NOT NULL,
  "tool_name" text NULL,
  "risk_policy_id" uuid NULL,
  "risk_result_id" uuid NULL,
  "chat_id" uuid NULL,
  "chat_message_id" uuid NULL,
  "feedback" text NULL,
  "feedback_user_id" text NULL,
  "feedback_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "tool_call_blocks_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "tool_call_blocks_chat_message_id_fkey" FOREIGN KEY ("chat_message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "tool_call_blocks_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "tool_call_blocks_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "tool_call_blocks_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "tool_call_blocks_risk_result_id_fkey" FOREIGN KEY ("risk_result_id") REFERENCES "risk_results" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "tool_call_blocks_feedback_check" CHECK ((feedback IS NULL) OR (feedback = ANY (ARRAY['up'::text, 'down'::text])))
);
-- Create index "tool_call_blocks_chat_message_idx" to table: "tool_call_blocks"
CREATE INDEX "tool_call_blocks_chat_message_idx" ON "tool_call_blocks" ("project_id", "chat_message_id") WHERE ((chat_message_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "tool_call_blocks_project_created_idx" to table: "tool_call_blocks"
CREATE INDEX "tool_call_blocks_project_created_idx" ON "tool_call_blocks" ("project_id", "created_at" DESC) WHERE (deleted IS FALSE);
-- Set comment to table: "tool_call_blocks"
COMMENT ON TABLE "tool_call_blocks" IS 'Durable record of a blocked tool call or prompt. One row per hook-time block decision, carrying the exact reason shown to the agent. Backs the durable /blocks/:id page and its thumbs feedback. The risk_results / risk_policies foreign keys are nullable enrichment links — the page renders from this row alone.';
-- Set comment to column: "reason" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."reason" IS 'The exact agent-facing reason captured at block time, independent of any later risk_results mutation.';
-- Set comment to column: "risk_result_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."risk_result_id" IS 'Optional link to the risk_results finding for this block, backfilled when one is recorded.';
