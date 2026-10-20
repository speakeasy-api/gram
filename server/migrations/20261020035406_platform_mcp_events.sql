-- Create "platform_mcp_event_subscriptions" table
CREATE TABLE "platform_mcp_event_subscriptions" (
  "id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "connection_id" uuid NULL,
  "connection_generation" uuid NOT NULL,
  "client_id" text NOT NULL,
  "user_id" text NOT NULL,
  "event_name" text NOT NULL,
  "callback_url" text NOT NULL,
  "signing_secret" text NOT NULL,
  "previous_signing_secret" text NULL,
  "previous_secret_expires_at" timestamptz NULL,
  "generation" uuid NOT NULL,
  "subscribed_at" timestamptz NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "verified_at" timestamptz NOT NULL,
  "deleted_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_event_subscriptions_connection_id_fkey" FOREIGN KEY ("connection_id") REFERENCES "platform_mcp_connections" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "platform_mcp_event_subscriptions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "platform_mcp_event_subscriptions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "platform_mcp_event_subscriptions_connection_idx" to table: "platform_mcp_event_subscriptions"
CREATE INDEX "platform_mcp_event_subscriptions_connection_idx" ON "platform_mcp_event_subscriptions" ("connection_id");
-- Create index "platform_mcp_event_subscriptions_match_idx" to table: "platform_mcp_event_subscriptions"
CREATE INDEX "platform_mcp_event_subscriptions_match_idx" ON "platform_mcp_event_subscriptions" ("project_id", "event_name", "subscribed_at") WHERE (deleted_at IS NULL);
-- Create index "platform_mcp_event_subscriptions_retention_idx" to table: "platform_mcp_event_subscriptions"
CREATE INDEX "platform_mcp_event_subscriptions_retention_idx" ON "platform_mcp_event_subscriptions" ((LEAST(expires_at, deleted_at)), "id");
-- Set comment to column: "id" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."organization_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."connection_generation" IS '@access: confidential';
-- Set comment to column: "client_id" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."client_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."user_id" IS '@access: confidential';
-- Set comment to column: "event_name" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."event_name" IS '@access: confidential';
-- Set comment to column: "callback_url" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."callback_url" IS '@access: restricted';
-- Set comment to column: "signing_secret" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."signing_secret" IS 'Encrypted webhook signing key.
@access: secret-restricted';
-- Set comment to column: "previous_signing_secret" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."previous_signing_secret" IS 'Encrypted webhook signing key.
@access: secret-restricted';
-- Set comment to column: "previous_secret_expires_at" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."previous_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "generation" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."generation" IS '@access: confidential';
-- Set comment to column: "subscribed_at" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."subscribed_at" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."expires_at" IS '@access: confidential';
-- Set comment to column: "verified_at" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."verified_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_event_subscriptions"
COMMENT ON COLUMN "platform_mcp_event_subscriptions"."updated_at" IS '@access: confidential';
-- Create "platform_mcp_event_deliveries" table
CREATE TABLE "platform_mcp_event_deliveries" (
  "id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "subscription_id" text NULL,
  "subscription_generation" uuid NOT NULL,
  "event_id" uuid NOT NULL,
  "event_name" text NOT NULL,
  "request_id" uuid NOT NULL,
  "payload" jsonb NOT NULL,
  "status" text NOT NULL,
  "attempts" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_event_deliveries_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "platform_mcp_event_deliveries_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "platform_mcp_event_deliveries_subscription_id_fkey" FOREIGN KEY ("subscription_id") REFERENCES "platform_mcp_event_subscriptions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "platform_mcp_event_deliveries_occurrence_idx" to table: "platform_mcp_event_deliveries"
CREATE UNIQUE INDEX "platform_mcp_event_deliveries_occurrence_idx" ON "platform_mcp_event_deliveries" ("subscription_id", "subscription_generation", "event_id");
-- Create index "platform_mcp_event_deliveries_project_idx" to table: "platform_mcp_event_deliveries"
CREATE INDEX "platform_mcp_event_deliveries_project_idx" ON "platform_mcp_event_deliveries" ("project_id", "created_at");
-- Create index "platform_mcp_event_deliveries_retention_idx" to table: "platform_mcp_event_deliveries"
CREATE INDEX "platform_mcp_event_deliveries_retention_idx" ON "platform_mcp_event_deliveries" ("created_at", "id");
-- Set comment to column: "id" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."organization_id" IS '@access: confidential';
-- Set comment to column: "subscription_id" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."subscription_id" IS '@access: confidential';
-- Set comment to column: "subscription_generation" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."subscription_generation" IS '@access: confidential';
-- Set comment to column: "event_id" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."event_id" IS '@access: confidential';
-- Set comment to column: "event_name" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."event_name" IS '@access: confidential';
-- Set comment to column: "request_id" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."request_id" IS '@access: confidential';
-- Set comment to column: "payload" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."payload" IS 'Server-constructed event references only; excludes evidence and user content.
@access: confidential';
-- Set comment to column: "status" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."status" IS '@access: confidential';
-- Set comment to column: "attempts" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."attempts" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_event_deliveries"
COMMENT ON COLUMN "platform_mcp_event_deliveries"."updated_at" IS '@access: confidential';
