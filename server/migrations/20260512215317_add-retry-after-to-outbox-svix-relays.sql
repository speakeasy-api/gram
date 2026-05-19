-- Modify "outbox_svix_relays" table
ALTER TABLE "outbox_svix_relays" ADD COLUMN "retry_after" timestamptz NULL;
