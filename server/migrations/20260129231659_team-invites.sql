-- Create "team_invites" table
CREATE TABLE "team_invites" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NULL,
  "email" text NOT NULL,
  "invited_by_user_id" text NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "token" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "team_invites_invited_by_user_id_fkey" FOREIGN KEY ("invited_by_user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "team_invites_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "team_invites_email_check" CHECK ((email <> ''::text) AND (char_length(email) <= 255)),
  CONSTRAINT "team_invites_status_check" CHECK (status = ANY (ARRAY['pending'::text, 'accepted'::text, 'expired'::text, 'cancelled'::text])),
  CONSTRAINT "team_invites_token_check" CHECK ((token <> ''::text) AND (char_length(token) <= 64))
);
-- Create index "team_invites_organization_id_email_pending_key" to table: "team_invites"
CREATE UNIQUE INDEX "team_invites_organization_id_email_pending_key" ON "team_invites" ("organization_id", "email") WHERE ((deleted IS FALSE) AND (status = 'pending'::text) AND (organization_id IS NOT NULL));
-- Create index "team_invites_token_key" to table: "team_invites"
CREATE UNIQUE INDEX "team_invites_token_key" ON "team_invites" ("token") WHERE (deleted IS FALSE);
