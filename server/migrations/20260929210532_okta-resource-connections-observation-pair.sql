-- atlas:txmode none

-- Modify "okta_resource_connections" table
ALTER TABLE "okta_resource_connections" ADD CONSTRAINT "okta_resource_connections_observed_result_observed_at_check" CHECK ((observed_result IS NULL) = (observed_at IS NULL)) NOT VALID;
ALTER TABLE "okta_resource_connections" VALIDATE CONSTRAINT "okta_resource_connections_observed_result_observed_at_check";
