-- Create "memory_eval_runs" table
CREATE TABLE "memory_eval_runs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "status" text NOT NULL DEFAULT 'queued',
  "config" jsonb NOT NULL,
  "report" jsonb NULL,
  "error" text NULL,
  "cost_usd" numeric(12,4) NULL,
  "requested_by" text NOT NULL,
  "started_at" timestamptz NULL,
  "finished_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id")
);
-- Create index "memory_eval_runs_created_at_idx" to table: "memory_eval_runs"
CREATE INDEX "memory_eval_runs_created_at_idx" ON "memory_eval_runs" ("created_at" DESC, "id" DESC) WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."organization_id" IS 'Organization the eval corpus was loaded into, normally the demo organization.
@access: confidential';
-- Set comment to column: "status" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."status" IS 'queued, running, succeeded or failed.
@access: confidential';
-- Set comment to column: "config" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."config" IS 'Answer, judge, summary and digest models plus runs per arm, all Speakeasy-chosen.
@access: confidential';
-- Set comment to column: "report" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."report" IS 'Scored results per model and arm over the synthetic corpus: counts, rates, token totals, costs and synthetic secret and canary names.
@access: confidential';
-- Set comment to column: "error" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."error" IS 'Why a failed run stopped; may quote a model provider error.
@access: restricted';
-- Set comment to column: "cost_usd" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."cost_usd" IS 'Model spend the run reported across preparation, answers and grading.
@access: confidential';
-- Set comment to column: "requested_by" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."requested_by" IS 'User ID of the Speakeasy staff member who started the run.
@access: confidential';
-- Set comment to column: "started_at" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."started_at" IS '@access: confidential';
-- Set comment to column: "finished_at" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."finished_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "memory_eval_runs"
COMMENT ON COLUMN "memory_eval_runs"."deleted" IS '@access: confidential';
