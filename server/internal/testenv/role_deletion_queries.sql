-- name: SeedRoleDeletionPlugin :one
WITH project AS (
 INSERT INTO projects (organization_id, name, slug)
 VALUES (@organization_id, 'Cleanup project', @slug) RETURNING id
), plugin AS (
 INSERT INTO plugins (organization_id, project_id, name, slug)
 SELECT @organization_id, id, 'Preserved plugin', @slug FROM project RETURNING id, project_id
), toolset AS (
 INSERT INTO toolsets (organization_id, project_id, name, slug)
 SELECT @organization_id, project_id, 'Preserved toolset', @slug FROM plugin RETURNING id
), server AS (
 INSERT INTO plugin_servers (plugin_id, toolset_id, display_name)
 SELECT plugin.id, toolset.id, 'Preserved content' FROM plugin, toolset
), skill AS (
 INSERT INTO skills (project_id, name, display_name)
 SELECT project_id, 'retained', 'Retained' FROM plugin RETURNING id
), distribution AS (
 INSERT INTO skill_distributions (project_id, skill_id, plugin_id, channel, created_by_user_id)
 SELECT plugin.project_id, skill.id, plugin.id, 'plugin', 'test-user' FROM plugin, skill
)
SELECT id FROM plugin;

-- name: SeedRoleDeletionAssignment :one
INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn)
VALUES (@plugin_id, @organization_id, @principal_urn) RETURNING id;

-- name: ArchiveRoleDeletionPlugin :exec
UPDATE plugins SET deleted_at = clock_timestamp() WHERE id = @id;

-- name: CountRoleDeletionPrincipalAssignments :one
SELECT count(*) FROM plugin_assignments WHERE plugin_id = @plugin_id AND principal_urn = @principal_urn;

-- name: ListRoleDeletionPrincipals :many
SELECT principal_urn FROM plugin_assignments WHERE plugin_id = @plugin_id ORDER BY principal_urn;

-- name: RoleDeletionContentSnapshot :one
SELECT jsonb_build_object(
 'plugins', (SELECT jsonb_agg(p ORDER BY id) FROM plugins p),
 'servers', (SELECT jsonb_agg(s ORDER BY id) FROM plugin_servers s),
 'toolsets', (SELECT jsonb_agg(t ORDER BY id) FROM toolsets t),
 'skills', (SELECT jsonb_agg(s ORDER BY id) FROM skills s),
 'distributions', (SELECT jsonb_agg(d ORDER BY id) FROM skill_distributions d)
)::text;

-- name: RoleDeletionAssignmentSnapshot :one
SELECT coalesce(jsonb_agg(a ORDER BY id), '[]'::jsonb)::text
FROM plugin_assignments a WHERE plugin_id = @plugin_id;

-- name: RoleDeletionAllAssignmentsSnapshot :one
SELECT coalesce(jsonb_agg(a ORDER BY id), '[]'::jsonb)::text FROM plugin_assignments a;

-- name: CountRoleDeletionAssignments :one
SELECT count(*) FROM plugin_assignments WHERE id = ANY(@ids::uuid[]);

-- name: SeedRoleDeletionGlobalRole :one
INSERT INTO global_roles (workos_slug, workos_name, workos_created_at, workos_updated_at, workos_last_event_id)
VALUES ('plugin-role', 'Plugin role', @event_time, @event_time, 'event_00SEED') RETURNING id;

-- name: GetRoleDeletionState :one
SELECT workos_deleted, workos_last_event_id FROM organization_roles r WHERE r.id = @id AND @scope::text = 'organization'
UNION ALL
SELECT workos_deleted, workos_last_event_id FROM global_roles r WHERE r.id = @id AND @scope::text = 'global';

-- name: InstallRoleDeletionFailure :exec
DO $install$
BEGIN
 CREATE FUNCTION reject_role_cleanup() RETURNS trigger LANGUAGE plpgsql AS $fn$
 BEGIN RAISE EXCEPTION 'cleanup failed'; END
 $fn$;
 CREATE TRIGGER reject_role_cleanup BEFORE DELETE ON plugin_assignments
 FOR EACH ROW EXECUTE FUNCTION reject_role_cleanup();
END
$install$;

-- name: ListRoleDeletionPluginAudits :many
SELECT a.organization_id, a.project_id, a.actor_id, a.actor_type, a.subject_id,
       a.subject_type, a.metadata, p.project_id AS plugin_project_id
FROM audit_logs a
JOIN plugins p ON a.subject_id = p.id::text
WHERE p.id = @plugin_id AND a.action = @action
ORDER BY a.id;

-- name: LockRoleDeletionAssignment :one
SELECT id FROM plugin_assignments WHERE id = @id FOR UPDATE;
