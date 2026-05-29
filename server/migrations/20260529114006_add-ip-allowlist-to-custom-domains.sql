-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD COLUMN "ip_allowlist" text[] NOT NULL DEFAULT '{}';
