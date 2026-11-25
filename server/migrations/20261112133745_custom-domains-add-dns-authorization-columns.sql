-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD COLUMN "dns_authorization_verified_at" timestamptz NULL, ADD COLUMN "dns_authorization_checked_at" timestamptz NULL;
-- Set comment to column: "dns_checked_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."dns_checked_at" IS 'Time of the routing DNS observation; matches health_checked_at only when both observations came from the same check.
@access: confidential';
-- Set comment to column: "dns_authorization_verified_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."dns_authorization_verified_at" IS 'Start of the current run of successful checks of the domain''s Certificate Manager DNS authorization record. NULL until a check succeeds; a failed check clears it. Reflects our own DNS lookup, not certificate issuance.
@access: confidential';
-- Set comment to column: "dns_authorization_checked_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."dns_authorization_checked_at" IS 'Time of the last check of the domain''s Certificate Manager DNS authorization record, whatever its result. NULL when it has never been checked.
@access: confidential';
