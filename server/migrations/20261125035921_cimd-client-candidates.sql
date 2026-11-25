-- Create "cimd_client_candidates" table
CREATE TABLE "cimd_client_candidates" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "url" text NOT NULL,
  "client_name" text NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "reason" text NULL,
  "reviewed_by" text NULL,
  "reviewed_at" timestamptz NULL,
  "document_id" uuid NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "cimd_client_candidates_document_id_fkey" FOREIGN KEY ("document_id") REFERENCES "ai_product_cimd_documents" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "cimd_client_candidates_url_check" CHECK (url <> ''::text)
);
-- Create index "cimd_client_candidates_status_first_seen_at_idx" to table: "cimd_client_candidates"
CREATE INDEX "cimd_client_candidates_status_first_seen_at_idx" ON "cimd_client_candidates" ("status", "first_seen_at" DESC);
-- Create index "cimd_client_candidates_url_key" to table: "cimd_client_candidates"
CREATE UNIQUE INDEX "cimd_client_candidates_url_key" ON "cimd_client_candidates" ("url");
-- Set comment to table: "cimd_client_candidates"
COMMENT ON TABLE "cimd_client_candidates" IS 'Staff review queue of CIMD client URLs no catalog document covers. One row per URL; rows are never deleted.';
-- Set comment to column: "id" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."id" IS '@access: confidential';
-- Set comment to column: "url" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."url" IS 'The client_id the client presented, stored exactly as received.
@access: restricted';
-- Set comment to column: "client_name" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."client_name" IS 'client_name from the fetched metadata document; null when absent.
@access: opaque-confidential';
-- Set comment to column: "status" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."status" IS 'One of pending, approved, dismissed, customer_specific; validated in application code.
@access: confidential';
-- Set comment to column: "reason" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."reason" IS 'Staff note recorded with the decision.
@access: opaque-restricted';
-- Set comment to column: "reviewed_by" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."reviewed_by" IS 'Subject URN of the staff member who decided; null while pending.
@access: confidential-pii';
-- Set comment to column: "reviewed_at" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."reviewed_at" IS '@access: confidential';
-- Set comment to column: "document_id" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."document_id" IS 'Catalog document created on approval; null otherwise.
@access: confidential';
-- Set comment to column: "first_seen_at" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "cimd_client_candidates"
COMMENT ON COLUMN "cimd_client_candidates"."updated_at" IS '@access: confidential';
-- Create "cimd_client_candidate_sightings" table
CREATE TABLE "cimd_client_candidate_sightings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "candidate_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "organization_id" text NULL,
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "cimd_client_candidate_sightings_candidate_id_fkey" FOREIGN KEY ("candidate_id") REFERENCES "cimd_client_candidates" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "cimd_client_candidate_sightings_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "cimd_client_candidate_sightings_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "cimd_client_candidate_sightings_candidate_issuer_key" to table: "cimd_client_candidate_sightings"
CREATE UNIQUE INDEX "cimd_client_candidate_sightings_candidate_issuer_key" ON "cimd_client_candidate_sightings" ("candidate_id", "user_session_issuer_id");
-- Create index "cimd_client_candidate_sightings_organization_id_idx" to table: "cimd_client_candidate_sightings"
CREATE INDEX "cimd_client_candidate_sightings_organization_id_idx" ON "cimd_client_candidate_sightings" ("organization_id");
-- Create index "cimd_client_candidate_sightings_user_session_issuer_id_idx" to table: "cimd_client_candidate_sightings"
CREATE INDEX "cimd_client_candidate_sightings_user_session_issuer_id_idx" ON "cimd_client_candidate_sightings" ("user_session_issuer_id");
-- Set comment to table: "cimd_client_candidate_sightings"
COMMENT ON TABLE "cimd_client_candidate_sightings" IS 'Issuers, and their organizations, that saw each CIMD client candidate.';
-- Set comment to column: "id" on table: "cimd_client_candidate_sightings"
COMMENT ON COLUMN "cimd_client_candidate_sightings"."id" IS '@access: confidential';
-- Set comment to column: "candidate_id" on table: "cimd_client_candidate_sightings"
COMMENT ON COLUMN "cimd_client_candidate_sightings"."candidate_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "cimd_client_candidate_sightings"
COMMENT ON COLUMN "cimd_client_candidate_sightings"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "cimd_client_candidate_sightings"
COMMENT ON COLUMN "cimd_client_candidate_sightings"."organization_id" IS 'The issuer''s organization; null for a platform-global issuer.
@access: confidential';
-- Set comment to column: "last_seen_at" on table: "cimd_client_candidate_sightings"
COMMENT ON COLUMN "cimd_client_candidate_sightings"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "cimd_client_candidate_sightings"
COMMENT ON COLUMN "cimd_client_candidate_sightings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "cimd_client_candidate_sightings"
COMMENT ON COLUMN "cimd_client_candidate_sightings"."updated_at" IS '@access: confidential';
