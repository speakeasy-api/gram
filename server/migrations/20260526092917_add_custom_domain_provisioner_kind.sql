-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD CONSTRAINT "custom_domains_provisioner_kind_check" CHECK (provisioner_kind = ANY (ARRAY['ingress'::text, 'gateway'::text])), ADD COLUMN "provisioner_kind" text NOT NULL DEFAULT 'ingress';
