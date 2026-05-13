-- atlas:nolint DS102
-- Create "outbox_relays" table
CREATE TABLE "outbox_relays" (
  "outbox_id" bigint NOT NULL,
  "processed_at" timestamptz NULL,
  "noop" boolean NOT NULL DEFAULT false,
  "dead_lettered" boolean NOT NULL DEFAULT false,
  "svix_message_id" text NULL,
  "attempts" integer NOT NULL DEFAULT 0,
  "last_error" text NULL,
  "retry_after" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("outbox_id"),
  CONSTRAINT "outbox_relays_outbox_id_fkey" FOREIGN KEY ("outbox_id") REFERENCES "outbox" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
) WITH (fillfactor = 80, autovacuum_vacuum_scale_factor = 0.05, autovacuum_analyze_scale_factor = 0.05);
-- Create index "outbox_relays_pending_idx" to table: "outbox_relays"
CREATE INDEX "outbox_relays_pending_idx" ON "outbox_relays" ("outbox_id") WHERE ((processed_at IS NULL) AND (dead_lettered IS FALSE));
-- Drop "outbox_svix_relays" table
DROP TABLE "outbox_svix_relays";
