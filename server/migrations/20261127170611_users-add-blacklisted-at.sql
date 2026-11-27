-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "blacklisted_at" timestamptz NULL;
-- Set comment to column: "blacklisted_at" on table: "users"
COMMENT ON COLUMN "users"."blacklisted_at" IS 'When staff blocked this user from the platform. A set value rejects every request the user makes or authorizes.
@access: confidential';
