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
