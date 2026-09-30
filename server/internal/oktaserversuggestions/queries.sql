-- The organization's live Okta connection. Exactly one per organization.
-- name: GetLiveConnection :one
SELECT
    c.id
  , c.status
  , o.applications_synced_at
FROM identity_provider_connections AS c
JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
 AND o.organization_id = c.organization_id
 AND o.deleted IS FALSE
WHERE c.organization_id = @organization_id
  AND c.provider = 'okta'
  AND c.deleted IS FALSE;

-- Active, assigned snapshot applications matched to the published catalog
-- entries whose Okta mapping names them. The mapping is unnested once into a
-- materialized set so the join hashes on the name instead of probing every
-- record per application. Invalid historical records may hold a non-array
-- mapping and are skipped here and again in Go. Records are fetched
-- separately, once per entry.
-- name: ListMappedApplications :many
WITH mapped AS MATERIALIZED (
    SELECT e.id AS registry_entry_id, n.name
    FROM mcp_registry_entries AS e
    CROSS JOIN LATERAL jsonb_array_elements_text(e.data #> '{_meta,com.speakeasy.ai/okta,oinNames}') AS n(name)
    WHERE e.published
      AND jsonb_typeof(e.data #> '{_meta,com.speakeasy.ai/okta,oinNames}') = 'array'
)
SELECT
    m.registry_entry_id
  , a.okta_app_id
  , a.label
  , a.name
  , a.sign_on_mode
  , (SELECT COUNT(*) FROM okta_application_assignments AS u
      WHERE u.organization_id = a.organization_id
        AND u.identity_provider_connection_id = a.identity_provider_connection_id
        AND u.okta_app_id = a.okta_app_id
        AND u.principal_kind = 'user'
        AND u.removed_at IS NULL)::integer AS user_assignments
  , (SELECT COUNT(*) FROM okta_application_assignments AS g
      WHERE g.organization_id = a.organization_id
        AND g.identity_provider_connection_id = a.identity_provider_connection_id
        AND g.okta_app_id = a.okta_app_id
        AND g.principal_kind = 'group'
        AND g.removed_at IS NULL)::integer AS group_assignments
FROM okta_applications AS a
JOIN mapped AS m ON m.name = a.name
WHERE a.organization_id = @organization_id
  AND a.identity_provider_connection_id = @identity_provider_connection_id
  AND a.removed_at IS NULL
  AND a.status = 'ACTIVE'
  AND EXISTS (
    SELECT 1 FROM okta_application_assignments AS s
    WHERE s.organization_id = a.organization_id
      AND s.identity_provider_connection_id = a.identity_provider_connection_id
      AND s.okta_app_id = a.okta_app_id
      AND s.removed_at IS NULL
  )
ORDER BY m.registry_entry_id, a.label, a.okta_app_id;

-- name: ListMappedEntries :many
SELECT id, data
FROM mcp_registry_entries
WHERE id = ANY(@ids::uuid[])
  AND published;

-- name: ListDismissals :many
SELECT registry_entry_id, created_at
FROM okta_server_suggestion_dismissals
WHERE organization_id = @organization_id;

-- Backends a live MCP server in one of the organization's projects fronts.
-- Tenancy through projects, since neither table has an organization column.
-- URLs compare without a trailing slash, as the resource connection readiness
-- does; the catalog and the admin may differ on it.
-- name: ListInstalledRemoteURLs :many
SELECT DISTINCT rtrim(r.url, '/')::text AS url
FROM remote_mcp_servers AS r
JOIN projects AS p ON p.id = r.project_id
JOIN mcp_servers AS ms ON ms.remote_mcp_server_id = r.id AND ms.project_id = r.project_id AND ms.deleted IS FALSE
WHERE p.organization_id = @organization_id
  AND p.deleted IS FALSE
  AND r.deleted IS FALSE
  AND rtrim(r.url, '/') = ANY(@urls::text[]);

-- name: UpsertDismissal :one
INSERT INTO okta_server_suggestion_dismissals (organization_id, registry_entry_id)
VALUES (@organization_id, @registry_entry_id)
ON CONFLICT (organization_id, registry_entry_id) DO UPDATE
SET updated_at = clock_timestamp()
RETURNING *;

-- name: DeleteDismissal :execrows
DELETE FROM okta_server_suggestion_dismissals
WHERE organization_id = @organization_id
  AND registry_entry_id = @registry_entry_id;

-- Test fixture: a snapshot row for an identity provider app instance.
-- name: CreateOktaApplicationFixture :one
INSERT INTO okta_applications (organization_id, identity_provider_connection_id, okta_app_id, label, name, sign_on_mode, status)
VALUES (@organization_id, @identity_provider_connection_id, @okta_app_id, @label, @name, @sign_on_mode, @status)
RETURNING id;

-- Test fixture: one live assignment for an app instance.
-- name: CreateOktaAssignmentFixture :exec
INSERT INTO okta_application_assignments (organization_id, identity_provider_connection_id, okta_app_id, principal_kind, okta_principal_id, assignment_scope)
VALUES (@organization_id, @identity_provider_connection_id, @okta_app_id, @principal_kind, @okta_principal_id, @assignment_scope);

-- Test fixture: removes every live assignment for an app instance.
-- name: RemoveOktaAssignmentsFixture :execrows
UPDATE okta_application_assignments
SET removed_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND okta_app_id = @okta_app_id
  AND removed_at IS NULL;

-- Test fixture: a remote MCP server backend in a project.
-- name: CreateRemoteBackendFixture :one
INSERT INTO remote_mcp_servers (project_id, name, slug, transport_type, url)
VALUES (@project_id, @name, @slug, 'streamable-http', @url)
RETURNING id;

-- Test fixture: an MCP server fronting a remote backend.
-- name: CreateMCPServerFixture :one
INSERT INTO mcp_servers (project_id, name, slug, remote_mcp_server_id, visibility)
VALUES (@project_id, @name, @slug, @remote_mcp_server_id, 'private')
RETURNING id;

-- Test fixture: soft-deletes an MCP server.
-- name: SoftDeleteMCPServerFixture :execrows
UPDATE mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id;

-- Test fixture: a client registered at an authorization server.
-- name: CreateIssuerClientFixture :one
INSERT INTO remote_session_clients (project_id, organization_id, remote_session_issuer_id, client_id, scope, resource_identifier)
VALUES (sqlc.narg(project_id), sqlc.narg(organization_id), @remote_session_issuer_id, @client_id, @scope::text[], sqlc.narg(resource_identifier))
RETURNING id;

-- name: GetIssuerFixture :one
SELECT id, issuer
FROM remote_session_issuers
WHERE id = @id;

-- Test fixture: the audit rows written for an organization, newest first.
-- name: ListAuditRowsFixture :many
SELECT action, subject_id, subject_type, subject_display_name, actor_id
FROM audit_logs
WHERE organization_id = @organization_id
ORDER BY created_at DESC;
