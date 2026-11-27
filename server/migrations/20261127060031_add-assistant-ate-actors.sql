-- Create "assistant_ate_actors" table
CREATE TABLE "assistant_ate_actors" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "runtime_id" uuid NOT NULL,
  "assistant_thread_id" uuid NOT NULL,
  "actor_name" text NOT NULL,
  "actor_uid" text NOT NULL,
  "state" text NOT NULL,
  "configured" boolean NOT NULL DEFAULT false,
  "workload_binding_json" jsonb NULL,
  "warm_until" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_ate_actors_assistant_fkey" FOREIGN KEY ("organization_id", "project_id", "assistant_id") REFERENCES "assistants" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_ate_actors_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_ate_actors_runtime_fkey" FOREIGN KEY ("project_id", "runtime_id") REFERENCES "assistant_runtimes" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_ate_actors_thread_fkey" FOREIGN KEY ("project_id", "assistant_thread_id") REFERENCES "assistant_threads" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_ate_actors_actor_name_check" CHECK ((actor_name <> ''::text) AND (char_length(actor_name) <= 63)),
  CONSTRAINT "assistant_ate_actors_actor_uid_check" CHECK (actor_uid <> ''::text),
  CONSTRAINT "assistant_ate_actors_state_check" CHECK ((state <> ''::text) AND (char_length(state) <= 50))
);
-- Create index "assistant_ate_actors_actor_uid_key" to table: "assistant_ate_actors"
CREATE UNIQUE INDEX "assistant_ate_actors_actor_uid_key" ON "assistant_ate_actors" ("actor_uid") WHERE (deleted IS FALSE);
-- Create index "assistant_ate_actors_project_runtime_idx" to table: "assistant_ate_actors"
CREATE INDEX "assistant_ate_actors_project_runtime_idx" ON "assistant_ate_actors" ("project_id", "runtime_id");
-- Create index "assistant_ate_actors_project_thread_idx" to table: "assistant_ate_actors"
CREATE INDEX "assistant_ate_actors_project_thread_idx" ON "assistant_ate_actors" ("project_id", "assistant_thread_id");
-- Create index "assistant_ate_actors_runtime_thread_active_key" to table: "assistant_ate_actors"
CREATE UNIQUE INDEX "assistant_ate_actors_runtime_thread_active_key" ON "assistant_ate_actors" ("runtime_id", "assistant_thread_id") WHERE (deleted IS FALSE);
-- Create index "assistant_ate_actors_state_warm_until_active_idx" to table: "assistant_ate_actors"
CREATE INDEX "assistant_ate_actors_state_warm_until_active_idx" ON "assistant_ate_actors" ("state", "warm_until") WHERE (deleted IS FALSE);
-- Set comment to column: "id" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."assistant_id" IS '@access: confidential';
-- Set comment to column: "runtime_id" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."runtime_id" IS '@access: confidential';
-- Set comment to column: "assistant_thread_id" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."assistant_thread_id" IS '@access: confidential';
-- Set comment to column: "actor_name" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."actor_name" IS 'DNS-1123 actor name within the configured ATE atespace.
@access: confidential';
-- Set comment to column: "actor_uid" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."actor_uid" IS 'Immutable ATE-assigned actor UID used for durable identity lookup.
@access: confidential';
-- Set comment to column: "state" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."state" IS 'Lifecycle state of the thread actor (Go constants are canonical).
@access: confidential';
-- Set comment to column: "configured" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."configured" IS 'True once initial bootstrap files have been delivered to the actor rootfs.
@access: confidential';
-- Set comment to column: "workload_binding_json" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."workload_binding_json" IS 'Serialized workload admission binding for this thread actor.
@access: restricted';
-- Set comment to column: "warm_until" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."warm_until" IS 'Deadline after which an idle running actor is eligible for synchronous suspend.
@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "assistant_ate_actors"
COMMENT ON COLUMN "assistant_ate_actors"."deleted" IS '@access: confidential';
