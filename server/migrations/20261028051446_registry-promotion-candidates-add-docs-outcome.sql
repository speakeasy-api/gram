-- Modify "registry_promotion_candidates" table
ALTER TABLE "registry_promotion_candidates" ADD COLUMN "docs_outcome" text NULL, ADD COLUMN "docs_review_since" timestamptz NULL;
-- Set comment to column: "docs_outcome" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_outcome" IS 'How guide delivery resolved: requested (factory issue filed), existing_guide (guide already on the setup-docs default branch) or guide_in_review (an open guide PR is pending). NULL on rows resolved before this was recorded means requested.
@access: confidential';
-- Set comment to column: "docs_review_since" on table: "registry_promotion_candidates"
COMMENT ON COLUMN "registry_promotion_candidates"."docs_review_since" IS 'When delivery first found an open guide PR for this slug; cleared on any other outcome. Drives the stalled-review alert.
@access: confidential';
