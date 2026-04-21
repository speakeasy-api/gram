CREATE TABLE "assistants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "name" text NOT NULL,
  "model" text NOT NULL,
  "instructions" text NOT NULL,
  "toolsets_json" jsonb NOT NULL DEFAULT '[]'::jsonb,
  "warm_ttl_seconds" bigint NOT NULL DEFAULT 300,
  "max_concurrency" bigint NOT NULL DEFAULT 1,
  "status" text NOT NULL DEFAULT 'active',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  CONSTRAINT "assistants_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "assistants_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistants_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 120)),
  CONSTRAINT "assistants_model_check" CHECK ((model <> ''::text) AND (char_length(model) <= 200)),
  CONSTRAINT "assistants_warm_ttl_seconds_check" CHECK ((warm_ttl_seconds >= 0) AND (warm_ttl_seconds <= 3600)),
  CONSTRAINT "assistants_max_concurrency_check" CHECK ((max_concurrency >= 1) AND (max_concurrency <= 100)),
  CONSTRAINT "assistants_status_check" CHECK (status = ANY (ARRAY['active'::text, 'paused'::text]))
);
CREATE UNIQUE INDEX "assistants_project_id_name_key" ON "assistants" ("project_id", "name") WHERE (deleted IS FALSE);

CREATE TABLE "assistant_threads" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "assistant_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "correlation_id" text NOT NULL,
  "chat_id" uuid NOT NULL,
  "source_kind" text NOT NULL,
  "source_ref_json" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "last_event_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  CONSTRAINT "assistant_threads_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "assistant_threads_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_threads_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_threads_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_threads_correlation_id_check" CHECK ((correlation_id <> ''::text) AND (char_length(correlation_id) <= 300)),
  CONSTRAINT "assistant_threads_source_kind_check" CHECK ((source_kind <> ''::text) AND (char_length(source_kind) <= 50))
);
CREATE UNIQUE INDEX "assistant_threads_project_id_assistant_id_correlation_id_key" ON "assistant_threads" ("project_id", "assistant_id", "correlation_id") WHERE (deleted IS FALSE);
CREATE INDEX "assistant_threads_assistant_id_last_event_at_idx" ON "assistant_threads" ("assistant_id", "last_event_at" DESC) WHERE (deleted IS FALSE);

CREATE TABLE "assistant_runtimes" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "assistant_thread_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "backend" text NOT NULL,
  "state" text NOT NULL,
  "warm_until" timestamptz NULL,
  "lease_owner" text NULL,
  "last_heartbeat_at" timestamptz NULL,
  "backend_metadata_json" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  CONSTRAINT "assistant_runtimes_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "assistant_runtimes_assistant_thread_id_fkey" FOREIGN KEY ("assistant_thread_id") REFERENCES "assistant_threads" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_runtimes_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_runtimes_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_runtimes_backend_check" CHECK ((backend <> ''::text) AND (char_length(backend) <= 50)),
  CONSTRAINT "assistant_runtimes_state_check" CHECK (state = ANY (ARRAY['starting'::text, 'active'::text, 'stopped'::text, 'failed'::text]))
);
CREATE UNIQUE INDEX "assistant_runtimes_assistant_thread_id_active_key" ON "assistant_runtimes" ("assistant_thread_id") WHERE ((deleted IS FALSE) AND (state = ANY (ARRAY['starting'::text, 'active'::text])));
CREATE INDEX "assistant_runtimes_assistant_id_state_idx" ON "assistant_runtimes" ("assistant_id", "state") WHERE (deleted IS FALSE);

CREATE TABLE "assistant_thread_events" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "assistant_thread_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "trigger_instance_id" uuid NULL,
  "event_id" text NOT NULL,
  "correlation_id" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "normalized_payload_json" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "source_payload_json" jsonb NOT NULL DEFAULT '{}'::jsonb,
  "attempts" bigint NOT NULL DEFAULT 0,
  "last_error" text NULL,
  "processed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  CONSTRAINT "assistant_thread_events_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "assistant_thread_events_assistant_thread_id_fkey" FOREIGN KEY ("assistant_thread_id") REFERENCES "assistant_threads" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_thread_events_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_thread_events_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_thread_events_trigger_instance_id_fkey" FOREIGN KEY ("trigger_instance_id") REFERENCES "trigger_instances" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_thread_events_event_id_check" CHECK ((event_id <> ''::text) AND (char_length(event_id) <= 300)),
  CONSTRAINT "assistant_thread_events_correlation_id_check" CHECK ((correlation_id <> ''::text) AND (char_length(correlation_id) <= 300)),
  CONSTRAINT "assistant_thread_events_status_check" CHECK (status = ANY (ARRAY['pending'::text, 'processing'::text, 'completed'::text, 'failed'::text]))
);
CREATE UNIQUE INDEX "assistant_thread_events_assistant_id_event_id_key" ON "assistant_thread_events" ("assistant_id", "event_id") WHERE (deleted IS FALSE);
CREATE INDEX "assistant_thread_events_thread_status_created_at_idx" ON "assistant_thread_events" ("assistant_thread_id", "status", "created_at") WHERE (deleted IS FALSE);
