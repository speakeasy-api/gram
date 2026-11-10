-- Modify "risk_results" table
ALTER TABLE "risk_results" ADD COLUMN "match_encrypted" text NULL, ADD COLUMN "spans_encrypted" text NULL;
-- Set comment to column: "match_encrypted" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."match_encrypted" IS 'match sealed with the organization''s risk-evidence data encryption key.
@access: secret-restricted';
-- Set comment to column: "spans_encrypted" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."spans_encrypted" IS 'spans, as JSON, sealed with the organization''s risk-evidence data encryption key.
@access: secret-restricted';
