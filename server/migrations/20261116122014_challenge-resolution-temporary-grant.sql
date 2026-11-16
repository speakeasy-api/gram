-- Modify "authz_challenge_resolutions" table
ALTER TABLE "authz_challenge_resolutions" ADD COLUMN "temporary_grant_id" uuid NULL;
-- Set comment to column: "resolution_type" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."resolution_type" IS 'How the challenge was resolved: role_assigned, temporary_grant, dismissed.
@access: confidential';
-- Set comment to column: "temporary_grant_id" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."temporary_grant_id" IS 'When resolution_type=temporary_grant, the principal_grants row that was granted with an expiry. Soft reference: the grant row may be revoked (deleted) later.
@access: confidential';
