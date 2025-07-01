-- Modify "http_security" table
ALTER TABLE "http_security" ADD COLUMN "oauth_types" text[] NULL, ADD COLUMN "oauth_flows" jsonb NULL;
