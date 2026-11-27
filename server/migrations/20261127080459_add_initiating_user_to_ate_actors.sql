-- Modify "assistant_ate_actors" table
ALTER TABLE "public"."assistant_ate_actors" ADD COLUMN "initiating_user_id" text NULL, ADD COLUMN "initiating_user_resolution" text NOT NULL DEFAULT 'not_recorded';
-- Set comment to column: "initiating_user_id" on table: "assistant_ate_actors"
COMMENT ON COLUMN "public"."assistant_ate_actors"."initiating_user_id" IS 'WorkOS user ID of the human who initiated this thread, or NULL for userless or unrecorded actors. Immutable after insert.
@access: confidential-pii';
-- Set comment to column: "initiating_user_resolution" on table: "assistant_ate_actors"
COMMENT ON COLUMN "public"."assistant_ate_actors"."initiating_user_resolution" IS 'How initiating_user_id was resolved at thread actor creation (Go constants are canonical; defaults to not_recorded for pre-existing rows). Immutable after insert.
@access: confidential';
