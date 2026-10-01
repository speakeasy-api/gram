-- name: SourceRoleDistributionSetupEnabled :one
-- Shared role-distribution lifecycle fixture: inspect committed or transaction-local source receipts.
SELECT EXISTS (SELECT 1 FROM organization_features WHERE organization_id = @organization_id AND feature_name = 'automatic-role-distribution' AND deleted IS FALSE);

-- name: SourceDisableRoleDistributionSetup :exec
-- Shared role-distribution lifecycle fixture: prepare lifecycle state without running the worker.
UPDATE organization_features SET deleted_at = clock_timestamp() WHERE organization_id = @organization_id AND feature_name = 'automatic-role-distribution';

-- name: SourceInsertGlobalRoleWithoutDistribution :exec
-- Simulate a role invisible to a concurrent organization source transaction.
INSERT INTO global_roles (workos_slug,workos_name,workos_created_at,workos_updated_at) VALUES (@slug,@slug,clock_timestamp(),clock_timestamp());

-- name: SourceLastOutboxID :one
SELECT COALESCE(max(id),0)::bigint FROM publish_outbox;

-- name: BootstrapSetGlobalRoleInactive :exec
UPDATE global_roles
SET deleted_at = CASE WHEN @workos_deleted::boolean THEN NULL ELSE clock_timestamp() END,
    workos_deleted_at = CASE WHEN @workos_deleted::boolean THEN clock_timestamp() ELSE NULL END
WHERE id = @id;

-- name: BootstrapSetOrganizationRoleInactive :exec
UPDATE organization_roles
SET deleted_at = CASE WHEN @workos_deleted::boolean THEN NULL ELSE clock_timestamp() END,
    workos_deleted_at = CASE WHEN @workos_deleted::boolean THEN clock_timestamp() ELSE NULL END
WHERE 'role:organization:' || id::text = @role_urn::text;

-- name: SourceMoveOrganizationRole :exec
UPDATE organization_roles SET organization_id = @organization_id WHERE 'role:organization:' || id::text = @role_urn::text;
