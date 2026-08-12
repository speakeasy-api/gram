-- Modify "litellm_instances" table
ALTER TABLE "litellm_instances" ADD COLUMN "last_guardrail_event_at" timestamptz NULL, ADD COLUMN "last_otel_event_at" timestamptz NULL, ADD COLUMN "last_error_at" timestamptz NULL, ADD COLUMN "last_error_kind" text NULL, ADD COLUMN "reported_litellm_version" text NULL, ADD COLUMN "reported_litellm_version_at" timestamptz NULL;
