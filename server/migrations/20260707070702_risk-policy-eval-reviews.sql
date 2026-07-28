-- Create "risk_policy_eval_reviews" table
CREATE TABLE "risk_policy_eval_reviews" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "risk_policy_id" uuid NOT NULL,
  "risk_policy_version" bigint NOT NULL,
  "chat_id" uuid NOT NULL,
  "verdict" text NOT NULL,
  "reviewed_by" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_policy_eval_reviews_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_eval_reviews_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_eval_reviews_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_eval_reviews_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_eval_reviews_verdict_check" CHECK (verdict = ANY (ARRAY['correct'::text, 'false_positive'::text, 'missed'::text]))
);
-- Create index "risk_policy_eval_reviews_policy_chat_reviewer_key" to table: "risk_policy_eval_reviews"
CREATE UNIQUE INDEX "risk_policy_eval_reviews_policy_chat_reviewer_key" ON "risk_policy_eval_reviews" ("project_id", "risk_policy_id", "chat_id", "reviewed_by") WHERE (deleted IS FALSE);
