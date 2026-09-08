-- Modify "audit_logs" table
ALTER TABLE "audit_logs" ADD COLUMN "acting_surface" text NULL, ADD COLUMN "acting_client_id" text NULL;
