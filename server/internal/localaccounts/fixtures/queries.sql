-- name: SetTrialDeadline :exec
UPDATE trials SET ends_at=$2 WHERE organization_id=$1;

-- name: CreateDetachedOrganization :exec
INSERT INTO organization_metadata(id,name,slug) VALUES($1,'Detached fixture','detached-fixture');

-- name: DeleteOrganization :exec
DELETE FROM organization_metadata WHERE id=$1;

-- name: CreateReplacementOrganization :exec
INSERT INTO organization_metadata(id,name,slug) VALUES($1,'Replacement','replacement-fixture');

-- name: ClearAccountTier :exec
UPDATE organization_metadata SET whitelisted=false,gram_account_type='free' WHERE id=$1;

-- name: DisableFeature :exec
UPDATE organization_features SET deleted_at=clock_timestamp() WHERE organization_id=$1 AND feature_name=$2;

-- name: CreateTrial :exec
INSERT INTO trials(organization_id,tier,ends_at) VALUES($1,'enterprise',$2);

-- name: SetTrialExpiration :exec
UPDATE trials SET ends_at=$2,demoted_at=$3 WHERE organization_id=$1;

-- name: CreateDisabledFeature :exec
INSERT INTO organization_features(organization_id,feature_name,deleted_at) VALUES($1,$2,clock_timestamp());

-- name: CreateAdminFeatures :exec
INSERT INTO organization_features(organization_id,feature_name,deleted_at) VALUES(sqlc.arg(organization_id),sqlc.arg(disabled_feature),clock_timestamp()),(sqlc.arg(organization_id),sqlc.arg(enabled_feature),NULL);

-- name: RenameOrganization :exec
UPDATE organization_metadata SET name='must roll back' WHERE id=$1;

-- name: LocalSchemaExists :one
SELECT (to_regnamespace('gram_local') IS NOT NULL)::boolean AS exists;

-- name: CountDetachedProfiles :one
SELECT count(*) FROM gram_local.account_profiles WHERE organization_id IS NULL;

-- name: GetTrialDates :one
SELECT ends_at, converted_at FROM trials WHERE organization_id=$1;

-- name: FeatureEnabled :one
SELECT EXISTS(SELECT 1 FROM organization_features WHERE organization_id=$1 AND feature_name=$2 AND NOT deleted);

-- name: SnapshotProjects :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM projects t;

-- name: SnapshotApiKeys :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM api_keys t;

-- name: SnapshotUsers :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM users t;

-- name: SnapshotOrganizationUserRelationships :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM organization_user_relationships t;

-- name: SnapshotOrganizationRoles :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM organization_roles t;

-- name: SnapshotOrganizationRoleAssignments :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM organization_role_assignments t;

-- name: SnapshotPrincipalGrants :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM principal_grants t;

-- name: SnapshotOrganizationMetadata :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM organization_metadata t;

-- name: SnapshotTrials :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM trials t;

-- name: SnapshotOrganizationFeatures :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM organization_features t;

-- name: SnapshotAccountProfiles :one
SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text AS snapshot FROM gram_local.account_profiles t;
