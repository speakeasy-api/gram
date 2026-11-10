-- atlas:txmode none

-- Create index "organization_features_organization_id_feature_name_idx" to table: "organization_features"
CREATE INDEX CONCURRENTLY "organization_features_organization_id_feature_name_idx" ON "organization_features" ("organization_id", "feature_name");
