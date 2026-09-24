-- Modify "support_matrix_integration_methods" table
ALTER TABLE "support_matrix_integration_methods" ADD COLUMN "account_eligibility" jsonb NOT NULL DEFAULT '{}';
-- Set comment to column: "account_eligibility" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."account_eligibility" IS 'Application-validated object keyed by account type (personal, team, enterprise) with values supported, unsupported, or unknown. An absent key is unknown. Eligibility is assessed separately from capability coverage: an ineligible account never has coverage, an eligible one is not thereby covered.';
-- Modify "support_matrix_method_platforms" table
ALTER TABLE "support_matrix_method_platforms" ADD COLUMN "account_eligibility" jsonb NOT NULL DEFAULT '{}';
-- Set comment to column: "account_eligibility" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."account_eligibility" IS 'Same shape as the method-level column, holding only the account types this platform differs on. An absent key inherits the method''s eligibility rather than meaning unknown.';
