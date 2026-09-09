-- Modify "remote_sessions" table
ALTER TABLE "remote_sessions" ADD COLUMN "upstream_subject" text NULL, ADD COLUMN "upstream_email" text NULL, ADD COLUMN "upstream_display_name" text NULL, ADD COLUMN "identity_source" text NULL, ADD COLUMN "enrichment" jsonb NULL, ADD COLUMN "last_validated_at" timestamptz NULL, ADD COLUMN "validation_status" text NULL, ADD COLUMN "validation_reason" text NULL;
