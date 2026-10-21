-- Create "google_chat_connections" table
CREATE TABLE "google_chat_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "google_subject" text NOT NULL,
  "email" text NOT NULL,
  "credentials_encrypted" text NULL,
  "granted_scopes" text[] NOT NULL DEFAULT '{}',
  "generation" uuid NOT NULL DEFAULT generate_uuidv7(),
  "status" text NOT NULL DEFAULT 'connected',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "google_chat_connections_organization_id_google_subject_key" UNIQUE ("organization_id", "google_subject"),
  CONSTRAINT "google_chat_connections_organization_id_id_key" UNIQUE ("organization_id", "id"),
  CONSTRAINT "google_chat_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "google_chat_connections_status_check" CHECK (status = ANY (ARRAY['connected'::text, 'reconnect_required'::text, 'disconnected'::text]))
);
-- Set comment to column: "id" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."organization_id" IS '@access: confidential';
-- Set comment to column: "google_subject" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."google_subject" IS '@access: confidential-pii';
-- Set comment to column: "email" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."email" IS '@access: confidential-pii';
-- Set comment to column: "credentials_encrypted" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."credentials_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "granted_scopes" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."granted_scopes" IS '@access: confidential';
-- Set comment to column: "generation" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."generation" IS '@access: confidential';
-- Set comment to column: "status" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."status" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "google_chat_connections"
COMMENT ON COLUMN "google_chat_connections"."updated_at" IS '@access: confidential';
-- Create "google_chat_bindings" table
CREATE TABLE "google_chat_bindings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "connection_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "trigger_id" uuid NULL,
  "space_name" text NOT NULL,
  "display_name" text NOT NULL,
  "prefix" text NOT NULL DEFAULT 'assistant:',
  "subscription_name" text NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "last_error" text NULL,
  "revision" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "google_chat_bindings_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "google_chat_bindings_organization_id_connection_id_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "google_chat_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "google_chat_bindings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "google_chat_bindings_trigger_id_fkey" FOREIGN KEY ("trigger_id") REFERENCES "trigger_instances" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "google_chat_bindings_status_check" CHECK (status = ANY (ARRAY['pending'::text, 'active'::text, 'paused'::text, 'error'::text, 'deleted'::text]))
);
-- Create index "google_chat_bindings_space_key" to table: "google_chat_bindings"
CREATE UNIQUE INDEX "google_chat_bindings_space_key" ON "google_chat_bindings" ("connection_id", "space_name") WHERE (status <> 'deleted'::text);
-- Set comment to column: "id" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."project_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."connection_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."assistant_id" IS '@access: confidential';
-- Set comment to column: "trigger_id" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."trigger_id" IS '@access: confidential';
-- Set comment to column: "space_name" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."space_name" IS '@access: restricted';
-- Set comment to column: "display_name" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."display_name" IS '@access: opaque-confidential';
-- Set comment to column: "prefix" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."prefix" IS '@access: opaque-confidential';
-- Set comment to column: "subscription_name" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."subscription_name" IS '@access: restricted';
-- Set comment to column: "status" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."status" IS '@access: confidential';
-- Set comment to column: "last_error" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."last_error" IS '@access: confidential';
-- Set comment to column: "revision" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."revision" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "google_chat_bindings"
COMMENT ON COLUMN "google_chat_bindings"."updated_at" IS '@access: confidential';
-- Create "google_chat_members" table
CREATE TABLE "google_chat_members" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "binding_id" uuid NOT NULL,
  "google_user_name" text NOT NULL,
  "display_name" text NOT NULL DEFAULT '',
  "email" text NOT NULL DEFAULT '',
  "user_id" text NULL,
  "active" boolean NOT NULL DEFAULT true,
  "revision" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "google_chat_members_binding_id_google_user_name_key" UNIQUE ("binding_id", "google_user_name"),
  CONSTRAINT "google_chat_members_binding_id_fkey" FOREIGN KEY ("binding_id") REFERENCES "google_chat_bindings" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "google_chat_members_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "google_chat_members_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION
);
-- Set comment to column: "id" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."project_id" IS '@access: confidential';
-- Set comment to column: "binding_id" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."binding_id" IS '@access: confidential';
-- Set comment to column: "google_user_name" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."google_user_name" IS '@access: confidential-pii';
-- Set comment to column: "display_name" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."display_name" IS '@access: confidential-pii';
-- Set comment to column: "email" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."email" IS '@access: confidential-pii';
-- Set comment to column: "user_id" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."user_id" IS '@access: confidential-pii';
-- Set comment to column: "active" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."active" IS '@access: confidential';
-- Set comment to column: "revision" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."revision" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "google_chat_members"
COMMENT ON COLUMN "google_chat_members"."updated_at" IS '@access: confidential';
