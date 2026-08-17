-- Modify "audit_logs" table
ALTER TABLE "audit_logs" ADD COLUMN "acting_surface" text NOT NULL DEFAULT 'unknown', ADD COLUMN "acting_client_id" text NULL;
