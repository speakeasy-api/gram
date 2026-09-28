-- Organization-scoped intent is serialized on the pre-existing organization row,
-- including when no settings exist yet. Plugin mutations remain project scoped.
-- Do not lock the organization key: an audience writer already holding the
-- admission/plugin locks inserts assignments whose FK takes KEY SHARE here.
-- NO KEY UPDATE is still an exclusive mutex for configuration/reconciliation,
-- but permits that FK check and avoids an organization -> admission -> FK cycle.
-- name: LockOrganization :one
SELECT id FROM organization_metadata WHERE id = @organization_id FOR NO KEY UPDATE;

-- name: GetSettings :one
SELECT organization_id, COALESCE(enabled, false)::boolean AS enabled,
 project_id, COALESCE(version, 0)::bigint AS version
FROM organization_role_provisioning_settings WHERE organization_id = @organization_id;

-- name: SaveSettings :one
INSERT INTO organization_role_provisioning_settings (organization_id, enabled, project_id, version)
VALUES (@organization_id, @enabled, sqlc.narg(project_id), @version)
ON CONFLICT (organization_id) DO UPDATE SET enabled = EXCLUDED.enabled,
 project_id = EXCLUDED.project_id, version = EXCLUDED.version, updated_at = clock_timestamp()
RETURNING COALESCE(version, 0)::bigint AS version;

-- name: ListProjects :many
SELECT p.id FROM projects p
LEFT JOIN plugins pl ON pl.project_id = p.id AND pl.organization_id = p.organization_id AND pl.deleted IS FALSE
WHERE p.organization_id = @organization_id AND p.deleted IS FALSE
GROUP BY p.id ORDER BY count(pl.id) DESC, p.created_at ASC, p.id ASC;

-- name: LockProject :one
SELECT id FROM projects WHERE id = @project_id AND organization_id = @organization_id AND deleted IS FALSE FOR UPDATE;

-- name: LockOrganizationRole :one
SELECT workos_name FROM organization_roles
WHERE id = @id AND organization_id = @organization_id AND deleted IS FALSE AND workos_deleted IS FALSE FOR UPDATE;

-- name: LockGlobalRole :one
SELECT workos_name FROM global_roles WHERE id = @id AND deleted IS FALSE AND workos_deleted IS FALSE FOR SHARE;

-- name: GetRoleSetting :one
SELECT * FROM role_provisioning_settings WHERE organization_id = @organization_id AND role_urn = @role_urn;

-- name: EnsureRoleSetting :exec
INSERT INTO role_provisioning_settings (organization_id, role_urn, enabled, project_id)
VALUES (@organization_id, @role_urn, @enabled, sqlc.narg(project_id))
ON CONFLICT (organization_id, role_urn) WHERE organization_id IS NOT NULL DO NOTHING;

-- name: SaveRoleSetting :exec
UPDATE role_provisioning_settings SET enabled = @enabled, project_id = sqlc.narg(project_id), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND role_urn = @role_urn;

-- name: RecordAttempt :exec
UPDATE role_provisioning_settings SET last_attempt_at = clock_timestamp(), last_error_code = sqlc.narg(error_code), updated_at = clock_timestamp()
WHERE organization_id = @organization_id AND role_urn = @role_urn;

-- name: ListAssociations :many
SELECT a.* FROM role_plugin_associations a
JOIN role_provisioning_settings s ON s.id = a.role_provisioning_setting_id
WHERE s.organization_id = @organization_id AND s.role_urn = @role_urn AND a.retired_at IS NULL;

-- name: RetireAssociation :exec
UPDATE role_plugin_associations a SET retired_at = clock_timestamp(), is_current = false, updated_at = clock_timestamp()
FROM role_provisioning_settings s
WHERE a.id = @id AND a.role_provisioning_setting_id = s.id AND s.organization_id = @organization_id
 AND a.project_id = @project_id;

-- name: ClearCurrentAssociation :exec
UPDATE role_plugin_associations a SET is_current = false, updated_at = clock_timestamp()
FROM role_provisioning_settings s WHERE a.role_provisioning_setting_id = s.id
AND s.organization_id = @organization_id AND s.role_urn = @role_urn AND a.is_current;

-- name: SetCurrentAssociation :exec
UPDATE role_plugin_associations a SET is_current = true, updated_at = clock_timestamp()
FROM role_provisioning_settings s WHERE a.id = @id AND a.role_provisioning_setting_id = s.id
AND s.organization_id = @organization_id AND a.project_id = @project_id AND a.retired_at IS NULL;

-- name: CreateAssociation :one
INSERT INTO role_plugin_associations (role_provisioning_setting_id, project_id, plugin_id, last_automatic_name)
SELECT s.id, p.project_id, p.id, p.name FROM role_provisioning_settings s
JOIN plugins p ON p.organization_id = s.organization_id
WHERE s.organization_id = @organization_id AND s.role_urn = @role_urn AND p.project_id = @project_id AND p.id = @plugin_id AND p.deleted IS FALSE
RETURNING *;

-- name: CreateRolePlugin :one
INSERT INTO plugins (organization_id, project_id, name, slug, is_default)
VALUES (@organization_id, @project_id, @name, @slug, false)
ON CONFLICT (organization_id, project_id, slug) WHERE deleted IS FALSE DO NOTHING
RETURNING *;

-- name: RenameRolePlugin :exec
UPDATE plugins SET name = @name, updated_at = clock_timestamp()
WHERE id = @plugin_id AND project_id = @project_id AND organization_id = @organization_id AND deleted IS FALSE AND name = @previous_name;

-- name: SetAutomaticName :exec
UPDATE role_plugin_associations a SET last_automatic_name = sqlc.narg(name), updated_at = clock_timestamp()
FROM plugins p WHERE a.plugin_id = p.id AND a.project_id = p.project_id
AND p.id = @plugin_id AND p.project_id = @project_id AND p.organization_id = @organization_id AND p.deleted IS FALSE;

-- name: GetRolloutTarget :one
-- Resolve feature-provider I/O before opening the reconciliation transaction.
-- The locked transition checks this project still matches current intent.
SELECT o.slug AS organization_slug, p.id AS project_id
FROM organization_metadata o
JOIN organization_role_provisioning_settings c ON c.organization_id = o.id
LEFT JOIN role_provisioning_settings s ON s.organization_id = o.id AND s.role_urn = @role_urn
JOIN projects p ON p.id = CASE WHEN s.id IS NULL THEN c.project_id ELSE s.project_id END
 AND p.organization_id = o.id AND p.deleted IS FALSE
WHERE o.id = @organization_id;

-- name: ListRoleSettings :many
SELECT role_urn, enabled, project_id FROM role_provisioning_settings
WHERE organization_id = @organization_id ORDER BY role_urn;
