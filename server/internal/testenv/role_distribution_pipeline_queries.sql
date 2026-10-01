-- name: PipelineEnableRoleDistribution :exec
INSERT INTO organization_features (organization_id, feature_name)
VALUES (@organization_id, 'automatic-role-distribution');

-- name: PipelineCountRoleDistributionOutbox :one
SELECT count(*) FROM publish_outbox
WHERE topic = 'gram.role_distribution.v1.RoleDistributionSetupRequestedV1';

-- name: PipelineCountOrganizationRole :one
SELECT count(*) FROM organization_roles WHERE id = @id;

-- name: PipelineCountPlugins :one
SELECT count(*) FROM plugins WHERE project_id = @project_id;

-- name: PipelineEngineeringPlugin :one
SELECT id FROM plugins WHERE project_id = @project_id AND name = 'Engineering';

-- name: PipelinePluginPrincipal :one
SELECT principal_urn FROM plugin_assignments WHERE plugin_id = @plugin_id;

-- name: PipelineRenamePlugin :exec
UPDATE plugins SET name = 'Administrator edit' WHERE id = @id AND project_id = @project_id;

-- name: PipelineDeletePluginAssignments :exec
DELETE FROM plugin_assignments
WHERE plugin_id = @plugin_id
  AND plugin_id IN (SELECT id FROM plugins WHERE project_id = @project_id);

-- name: PipelineCountPluginAssignments :one
SELECT count(*) FROM plugin_assignments WHERE plugin_id = @plugin_id;

-- name: PipelineInstallPublicationFailure :exec
-- Reject publication outbox inserts to prove plugin writes roll back with publication failure.
-- Installed only in the disposable pipeline test database.
DO $install$
BEGIN
    CREATE FUNCTION reject_pipeline_publication() RETURNS trigger LANGUAGE plpgsql AS $fn$
    BEGIN
        IF NEW.topic = 'gram.plugins.v1.PublicationRequested' THEN
            RAISE EXCEPTION 'injected publication failure';
        END IF;
        RETURN NEW;
    END
    $fn$;
    CREATE TRIGGER reject_pipeline_publication BEFORE INSERT ON publish_outbox
        FOR EACH ROW EXECUTE FUNCTION reject_pipeline_publication();
END
$install$;

-- name: RolloutInsertGlobalRoles :exec
-- Include more than one page, and deleted roles that must not receive setup requests.
INSERT INTO global_roles (workos_slug, workos_name, workos_created_at, workos_updated_at, deleted_at)
SELECT 'rollout-global-' || n, 'Rollout Global ' || n, clock_timestamp(), clock_timestamp(),
CASE WHEN n > 103 THEN clock_timestamp() ELSE NULL END
FROM generate_series(1, 105) n;

-- name: RolloutInsertLocalRoles :exec
-- Simulate pre-rollout roles created while distribution was disabled; pagination must discover them.
INSERT INTO organization_roles (organization_id, workos_slug, workos_name, workos_created_at, workos_updated_at, deleted_at)
SELECT @organization_id, 'rollout-local-' || n, 'Rollout Local ' || n, clock_timestamp(), clock_timestamp(),
CASE WHEN n > 103 THEN clock_timestamp() ELSE NULL END
FROM generate_series(1, 105) n;

-- name: RolloutRejectOutbox :exec
-- Prove rollout flag rolls back if enqueue fails.
ALTER TABLE publish_outbox ADD CONSTRAINT reject_rollout_outbox CHECK (topic != 'gram.role_distribution.v1.RoleDistributionSetupRequestedV1') NOT VALID;

-- name: RolloutBlockedBackends :many
-- Observe real lock dependencies without relying on production query text.
SELECT pid FROM pg_catalog.pg_stat_activity
WHERE datname = current_database() AND @blocker_pid::integer = ANY(pg_catalog.pg_blocking_pids(pid));

-- name: RolloutActiveRoles :many
SELECT ('role:global:' || id)::text AS role_urn FROM global_roles WHERE deleted IS FALSE AND workos_deleted IS FALSE
UNION ALL
SELECT ('role:organization:' || id)::text AS role_urn FROM organization_roles WHERE organization_id = @organization_id AND deleted IS FALSE AND workos_deleted IS FALSE;
