-- Modify "network_ingresses" table
ALTER TABLE "network_ingresses" ADD COLUMN "gateway_service_hostnames" text[] NOT NULL DEFAULT ARRAY[]::text[];
-- Set comment to column: "gateway_service_hostname" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."gateway_service_hostname" IS 'Unused; superseded by gateway_service_hostnames.
@access: restricted';
-- Set comment to column: "gateway_service_hostnames" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."gateway_service_hostnames" IS 'Labels of the Tailscale Services the gateway may have defined for this ingress; copies of current and former hostnames.
@access: restricted';
