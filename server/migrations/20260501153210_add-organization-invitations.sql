-- Create "organization_invitations" table
CREATE TABLE "organization_invitations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "email" text NOT NULL,
  "token" text NOT NULL,
  "inviter_user_id" text NOT NULL,
  "role_slug" text NULL,
  "state" text NOT NULL DEFAULT 'pending',
  "expires_at" timestamptz NOT NULL DEFAULT (clock_timestamp() + '7 days'::interval),
  "accepted_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_invitations_inviter_user_id_fkey" FOREIGN KEY ("inviter_user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "organization_invitations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "organization_invitations_email_check" CHECK (email <> ''::text),
  CONSTRAINT "organization_invitations_state_check" CHECK (state = ANY (ARRAY['pending'::text, 'accepted'::text, 'revoked'::text, 'expired'::text]))
);
-- Create index "organization_invitations_org_email_pending_key" to table: "organization_invitations"
CREATE UNIQUE INDEX "organization_invitations_org_email_pending_key" ON "organization_invitations" ("organization_id", "email") WHERE (state = 'pending'::text);
-- Create index "organization_invitations_token_key" to table: "organization_invitations"
CREATE UNIQUE INDEX "organization_invitations_token_key" ON "organization_invitations" ("token");
