-- Create "registry_promotion_candidates" table
CREATE TABLE "registry_promotion_candidates" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "mcp_server_id" uuid NULL,
  "remote_mcp_server_id" uuid NULL,
  "source_revision" text NOT NULL,
  "eligible" boolean NOT NULL DEFAULT false,
  "reason" text NOT NULL,
  "observed_successes" bigint NOT NULL DEFAULT 0,
  "observed_failures" bigint NOT NULL DEFAULT 0,
  "observed_days" integer NOT NULL DEFAULT 0,
  "checked_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "registry_entry_id" uuid NULL,
  "public_metadata" jsonb NULL,
  "endpoint_identity" text NULL,
  "request_digest" text NULL,
  "retry_key" text NULL,
  "promoted_by" text NULL,
  "promoted_at" timestamptz NULL,
  "docs_url" text NULL,
  "docs_queued_at" timestamptz NULL,
  "delivery_error" text NULL,
  "discovery_last_attempt_at" timestamptz NULL,
  "discovery_next_attempt_at" timestamptz NULL,
  "discovery_failures" integer NOT NULL DEFAULT 0,
  "docs_attempts" integer NOT NULL DEFAULT 0,
  "docs_failures" integer NOT NULL DEFAULT 0,
  "docs_last_attempt_at" timestamptz NULL,
  "docs_next_attempt_at" timestamptz NULL,
  "docs_needs_attention" boolean NOT NULL DEFAULT false,
  "docs_lease_until" timestamptz NULL,
  "docs_lease_token" uuid NULL,
  "docs_revision" uuid NOT NULL DEFAULT generate_uuidv7(),
  "docs_retry_key" text NULL,
  "docs_retry_revision" text NULL,
  "docs_retry_by" text NULL,
  "curation_status" text NOT NULL DEFAULT 'active',
  "curation_revision" uuid NOT NULL DEFAULT generate_uuidv7(),
  "curation_updated_at" timestamptz NULL,
  "curation_updated_by" text NULL,
  "curation_retry_key" text NULL,
  "curation_retry_revision" text NULL,
  "curation_retry_action" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "registry_promotion_candidates_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "registry_promotion_candidates_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "registry_promotion_candidates_registry_entry_id_fkey" FOREIGN KEY ("registry_entry_id") REFERENCES "mcp_registry_entries" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "registry_promotion_candidates_remote_mcp_server_id_fkey" FOREIGN KEY ("remote_mcp_server_id") REFERENCES "remote_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "registry_promotion_candidates_discovery_idx" to table: "registry_promotion_candidates"
CREATE INDEX "registry_promotion_candidates_discovery_idx" ON "registry_promotion_candidates" ("discovery_next_attempt_at", "id") WHERE (promoted_at IS NULL);
-- Create index "registry_promotion_candidates_docs_slug_key" to table: "registry_promotion_candidates"
CREATE UNIQUE INDEX "registry_promotion_candidates_docs_slug_key" ON "registry_promotion_candidates" (((public_metadata ->> 'docs_slug'::text))) WHERE (public_metadata IS NOT NULL);
-- Create index "registry_promotion_candidates_endpoint_key" to table: "registry_promotion_candidates"
CREATE UNIQUE INDEX "registry_promotion_candidates_endpoint_key" ON "registry_promotion_candidates" ("endpoint_identity") WHERE (endpoint_identity IS NOT NULL);
-- Create index "registry_promotion_candidates_newest_idx" to table: "registry_promotion_candidates"
CREATE INDEX "registry_promotion_candidates_newest_idx" ON "registry_promotion_candidates" ("created_at" DESC, "id" DESC);
-- Create index "registry_promotion_candidates_pending_idx" to table: "registry_promotion_candidates"
CREATE INDEX "registry_promotion_candidates_pending_idx" ON "registry_promotion_candidates" ("docs_next_attempt_at", "id") WHERE ((promoted_at IS NOT NULL) AND (docs_queued_at IS NULL) AND (docs_needs_attention IS FALSE));
-- Create index "registry_promotion_candidates_retry_key" to table: "registry_promotion_candidates"
CREATE UNIQUE INDEX "registry_promotion_candidates_retry_key" ON "registry_promotion_candidates" ("project_id", "retry_key") WHERE (retry_key IS NOT NULL);
-- Create index "registry_promotion_candidates_source_key" to table: "registry_promotion_candidates"
CREATE UNIQUE INDEX "registry_promotion_candidates_source_key" ON "registry_promotion_candidates" ("project_id", "mcp_server_id");
-- Set comment to column: "id" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."project_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "remote_mcp_server_id" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."remote_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "source_revision" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."source_revision" IS '@access: confidential';
-- Set comment to column: "eligible" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."eligible" IS '@access: confidential';
-- Set comment to column: "reason" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."reason" IS '@access: confidential';
-- Set comment to column: "observed_successes" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."observed_successes" IS '@access: confidential';
-- Set comment to column: "observed_failures" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."observed_failures" IS '@access: confidential';
-- Set comment to column: "observed_days" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."observed_days" IS '@access: confidential';
-- Set comment to column: "checked_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."checked_at" IS '@access: confidential';
-- Set comment to column: "registry_entry_id" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."registry_entry_id" IS '@access: confidential';
-- Set comment to column: "public_metadata" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."public_metadata" IS 'Staff-reviewed public catalogue and documentation input.
@access: opaque-restricted';
-- Set comment to column: "endpoint_identity" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."endpoint_identity" IS '@access: restricted';
-- Set comment to column: "request_digest" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."request_digest" IS '@access: confidential';
-- Set comment to column: "retry_key" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."retry_key" IS '@access: confidential';
-- Set comment to column: "promoted_by" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."promoted_by" IS '@access: confidential-pii';
-- Set comment to column: "promoted_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."promoted_at" IS '@access: confidential';
-- Set comment to column: "docs_url" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_url" IS '@access: restricted';
-- Set comment to column: "docs_queued_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_queued_at" IS '@access: confidential';
-- Set comment to column: "delivery_error" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."delivery_error" IS '@access: confidential';
-- Set comment to column: "discovery_last_attempt_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."discovery_last_attempt_at" IS '@access: confidential';
-- Set comment to column: "discovery_next_attempt_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."discovery_next_attempt_at" IS '@access: confidential';
-- Set comment to column: "discovery_failures" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."discovery_failures" IS '@access: confidential';
-- Set comment to column: "docs_attempts" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_attempts" IS '@access: confidential';
-- Set comment to column: "docs_failures" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_failures" IS '@access: confidential';
-- Set comment to column: "docs_last_attempt_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_last_attempt_at" IS '@access: confidential';
-- Set comment to column: "docs_next_attempt_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_next_attempt_at" IS '@access: confidential';
-- Set comment to column: "docs_needs_attention" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_needs_attention" IS '@access: confidential';
-- Set comment to column: "docs_lease_until" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_lease_until" IS '@access: confidential';
-- Set comment to column: "docs_lease_token" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_lease_token" IS '@access: confidential';
-- Set comment to column: "docs_revision" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_revision" IS '@access: confidential';
-- Set comment to column: "docs_retry_key" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_retry_key" IS '@access: confidential';
-- Set comment to column: "docs_retry_revision" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_retry_revision" IS '@access: confidential';
-- Set comment to column: "docs_retry_by" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_retry_by" IS '@access: confidential-pii';
-- Set comment to column: "curation_status" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."curation_status" IS 'Staff curation state, independent of observed eligibility.
@access: confidential';
-- Set comment to column: "curation_revision" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."curation_revision" IS 'Compare-and-swap revision for staff curation changes.
@access: confidential';
-- Set comment to column: "curation_updated_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."curation_updated_at" IS '@access: confidential';
-- Set comment to column: "curation_updated_by" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."curation_updated_by" IS '@access: confidential-pii';
-- Set comment to column: "curation_retry_key" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."curation_retry_key" IS '@access: confidential';
-- Set comment to column: "curation_retry_revision" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."curation_retry_revision" IS '@access: confidential';
-- Set comment to column: "curation_retry_action" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."curation_retry_action" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."updated_at" IS '@access: confidential';
