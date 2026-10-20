-- Create "assistant_kit_interrupt_requests" table
CREATE TABLE "assistant_kit_interrupt_requests" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "assistant_id" uuid NULL,
  "runtime_id" uuid NULL,
  "assistant_thread_id" uuid NULL,
  "event_id" uuid NULL,
  "attempt" bigint NOT NULL,
  "outcome" text NOT NULL DEFAULT 'pending',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "acknowledged_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_kit_interrupt_requests_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_interrupt_requests_assistant_thread_id_fkey" FOREIGN KEY ("assistant_thread_id") REFERENCES "assistant_threads" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_interrupt_requests_event_id_fkey" FOREIGN KEY ("event_id") REFERENCES "assistant_thread_events" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_interrupt_requests_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_kit_interrupt_requests_runtime_id_fkey" FOREIGN KEY ("runtime_id") REFERENCES "assistant_runtimes" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "assistant_kit_interrupt_requests_attempt_key" to table: "assistant_kit_interrupt_requests"
CREATE UNIQUE INDEX "assistant_kit_interrupt_requests_attempt_key" ON "assistant_kit_interrupt_requests" ("project_id", "runtime_id", "event_id", "attempt");
-- Set comment to column: "id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."assistant_id" IS '@access: confidential';
-- Set comment to column: "runtime_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."runtime_id" IS '@access: confidential';
-- Set comment to column: "assistant_thread_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."assistant_thread_id" IS '@access: confidential';
-- Set comment to column: "event_id" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."event_id" IS '@access: confidential';
-- Set comment to column: "attempt" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."attempt" IS '@access: confidential';
-- Set comment to column: "outcome" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."outcome" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."created_at" IS '@access: confidential';
-- Set comment to column: "acknowledged_at" on table: "assistant_kit_interrupt_requests"
COMMENT ON COLUMN "assistant_kit_interrupt_requests"."acknowledged_at" IS '@access: confidential';
