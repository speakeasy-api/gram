-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD COLUMN "provisioner_kind" text NOT NULL DEFAULT 'ingress';
