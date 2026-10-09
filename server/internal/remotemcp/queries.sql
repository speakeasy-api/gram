-- Remote MCP Servers

-- name: CreateServer :one
INSERT INTO remote_mcp_servers (id, project_id, name, slug, transport_type, url)
VALUES (@id, @project_id, @name, @slug, @transport_type, @url)
RETURNING *;

-- name: GetServerByID :one
SELECT *
FROM remote_mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE;

-- name: GetServerByIDForUpdate :one
-- GetServerByID holding a row lock until the transaction ends, so a
-- concurrent UpdateServer waits and a probe result is applied against the
-- URL that is current at write time.
SELECT *
FROM remote_mcp_servers
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
FOR UPDATE;

-- name: GetServerBySlug :one
SELECT *
FROM remote_mcp_servers
WHERE slug = @slug AND project_id = @project_id AND deleted IS FALSE;

-- name: ListServersByProjectID :many
SELECT *
FROM remote_mcp_servers
WHERE project_id = @project_id AND deleted IS FALSE
ORDER BY created_at DESC;

-- name: UpdateServer :one
UPDATE remote_mcp_servers
SET
    name = @name,
    slug = @slug,
    transport_type = COALESCE(@transport_type, transport_type),
    url = COALESCE(@url, url),
    updated_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- name: DeleteServer :one
UPDATE remote_mcp_servers
SET deleted_at = clock_timestamp()
WHERE id = @id AND project_id = @project_id AND deleted IS FALSE
RETURNING *;

-- Remote MCP Server Headers

-- The remote_mcp_server_headers table has no project_id column of its own, so
-- every management query below pins the project by subselecting the parent
-- remote_mcp_servers row. Without that, a caller could address another
-- project's header by guessing its id.

-- name: ListHeadersByServerID :many
-- Not project-scoped. Serves the MCP proxy (internal/mcp/serveendpoint.go),
-- which has already resolved the server row and needs decrypted header values
-- to inject into outbound requests. Management reads use ListServerHeaders.
SELECT *
FROM remote_mcp_server_headers
WHERE remote_mcp_server_id = @remote_mcp_server_id AND deleted IS FALSE
ORDER BY name;

-- name: ListServerHeaders :many
SELECT *
FROM remote_mcp_server_headers
WHERE remote_mcp_server_id = @remote_mcp_server_id
    AND deleted IS FALSE
    AND remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    )
ORDER BY name;

-- name: GetServerHeader :one
SELECT *
FROM remote_mcp_server_headers
WHERE remote_mcp_server_headers.id = @id
    AND remote_mcp_server_headers.deleted IS FALSE
    AND remote_mcp_server_headers.remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    );

-- name: ServerHeaderNameExists :one
-- Reports whether another live header of the server already uses name,
-- ignoring case and reading underscores as dashes, which is how the header
-- policy and some upstreams match names. The unique index compares names
-- exactly, and headers stored before names were canonicalized can differ only
-- in case, so writers check this while holding the parent server's row lock. exclude_id is the header
-- being updated, or the nil UUID on create.
SELECT EXISTS (
    SELECT 1
    FROM remote_mcp_server_headers
    WHERE remote_mcp_server_headers.remote_mcp_server_id = @remote_mcp_server_id
        AND remote_mcp_server_headers.deleted IS FALSE
        AND replace(lower(remote_mcp_server_headers.name), '_', '-') = replace(lower(@name::text), '_', '-')
        AND remote_mcp_server_headers.id <> @exclude_id
        AND remote_mcp_server_headers.remote_mcp_server_id IN (
            SELECT remote_mcp_servers.id FROM remote_mcp_servers
            WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
        )
) AS name_exists;

-- name: CreateServerHeader :one
-- Plain INSERT (never an upsert) so a live name collision raises a unique
-- violation the caller maps to 409 rather than silently overwriting the
-- existing header. The INSERT ... SELECT yields zero rows when the parent
-- server is missing or belongs to another project, which the caller maps to 404.
INSERT INTO remote_mcp_server_headers (
    remote_mcp_server_id,
    name,
    description,
    is_required,
    is_secret,
    value,
    value_from_request_header
)
SELECT
    remote_mcp_servers.id,
    @name::text,
    sqlc.narg(description)::text,
    @is_required::boolean,
    @is_secret::boolean,
    sqlc.narg(value)::text,
    sqlc.narg(value_from_request_header)::text
FROM remote_mcp_servers
WHERE remote_mcp_servers.id = @remote_mcp_server_id
    AND remote_mcp_servers.project_id = @project_id
    AND remote_mcp_servers.deleted IS FALSE
RETURNING *;

-- name: UpdateServerHeader :one
-- Full replace of the mutable fields, with one exception: when set_value is
-- false the caller omitted a value for an existing secret header, so the stored
-- ciphertext is left in place. Preserving it in SQL (rather than reading it out
-- and writing it back) keeps the encrypted value inside the database and makes
-- double-encryption impossible. It also keeps the row satisfying
-- remote_mcp_server_headers_value_source_check, which a plain
-- "value = NULL" write would violate for a secret header.
UPDATE remote_mcp_server_headers
SET
    name = @name::text,
    description = sqlc.narg(description)::text,
    is_required = @is_required::boolean,
    is_secret = @is_secret::boolean,
    value = CASE WHEN @set_value::boolean THEN sqlc.narg(value)::text ELSE value END,
    value_from_request_header = sqlc.narg(value_from_request_header)::text,
    updated_at = clock_timestamp()
WHERE remote_mcp_server_headers.id = @id
    AND remote_mcp_server_headers.deleted IS FALSE
    AND remote_mcp_server_headers.remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    )
RETURNING *;

-- name: DeleteServerHeader :one
-- Returns the soft-deleted row so the caller can emit an audit event carrying
-- the header's name.
UPDATE remote_mcp_server_headers
SET deleted_at = clock_timestamp()
WHERE remote_mcp_server_headers.id = @id
    AND remote_mcp_server_headers.deleted IS FALSE
    AND remote_mcp_server_headers.remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    )
RETURNING *;

-- name: DeleteHeadersByServerID :exec
-- Soft-delete every header of a server. The FK's ON DELETE CASCADE does not
-- fire for soft deletes, so deleteServer calls this explicitly. The affected
-- rows are not returned: the cascade is covered by the parent's
-- remote-mcp:delete audit entry and emits no per-header events. Runs before the
-- parent row is tombstoned, so the parent is still visible to the project
-- subselect.
UPDATE remote_mcp_server_headers
SET deleted_at = clock_timestamp()
WHERE remote_mcp_server_id = @remote_mcp_server_id
    AND deleted IS FALSE
    AND remote_mcp_server_id IN (
        SELECT remote_mcp_servers.id FROM remote_mcp_servers
        WHERE remote_mcp_servers.project_id = @project_id AND remote_mcp_servers.deleted IS FALSE
    );

-- Remote Protected Resources

-- name: UpsertRemoteProtectedResource :one
-- Records an RFC 9728 document read for resource_identifier. scopes_supported
-- and bearer_methods_supported stay NULL when the document omits them; an
-- empty array means the document advertised none. A successful read clears
-- the last fetch error.
INSERT INTO remote_protected_resources (
    project_id,
    organization_id,
    resource_identifier,
    metadata_url,
    authorization_servers,
    scopes_supported,
    bearer_methods_supported,
    resource_name,
    resource_documentation,
    resource_policy_uri,
    resource_tos_uri,
    dpop_bound_access_tokens_required,
    dpop_signing_alg_values_supported,
    tls_client_certificate_bound_access_tokens,
    metadata,
    metadata_fetched_at
)
VALUES (
    @project_id,
    @organization_id,
    @resource_identifier::text,
    NULLIF(@metadata_url::text, ''),
    sqlc.narg(authorization_servers)::text[],
    sqlc.narg(scopes_supported)::text[],
    sqlc.narg(bearer_methods_supported)::text[],
    NULLIF(@resource_name::text, ''),
    NULLIF(@resource_documentation::text, ''),
    NULLIF(@resource_policy_uri::text, ''),
    NULLIF(@resource_tos_uri::text, ''),
    sqlc.narg(dpop_bound_access_tokens_required)::boolean,
    sqlc.narg(dpop_signing_alg_values_supported)::text[],
    sqlc.narg(tls_client_certificate_bound_access_tokens)::boolean,
    NULLIF(@metadata::text, '')::jsonb,
    clock_timestamp()
)
ON CONFLICT (project_id, resource_identifier) WHERE deleted IS FALSE DO UPDATE
SET
    organization_id = EXCLUDED.organization_id,
    metadata_url = EXCLUDED.metadata_url,
    authorization_servers = EXCLUDED.authorization_servers,
    scopes_supported = EXCLUDED.scopes_supported,
    bearer_methods_supported = EXCLUDED.bearer_methods_supported,
    resource_name = EXCLUDED.resource_name,
    resource_documentation = EXCLUDED.resource_documentation,
    resource_policy_uri = EXCLUDED.resource_policy_uri,
    resource_tos_uri = EXCLUDED.resource_tos_uri,
    dpop_bound_access_tokens_required = EXCLUDED.dpop_bound_access_tokens_required,
    dpop_signing_alg_values_supported = EXCLUDED.dpop_signing_alg_values_supported,
    tls_client_certificate_bound_access_tokens = EXCLUDED.tls_client_certificate_bound_access_tokens,
    metadata = EXCLUDED.metadata,
    metadata_fetched_at = clock_timestamp(),
    metadata_last_error = NULL,
    metadata_last_error_at = NULL,
    updated_at = clock_timestamp()
RETURNING *;

-- name: UpsertRemoteProtectedResourceIfChanged :execrows
-- UpsertRemoteProtectedResource for a login's read: an existing row is only
-- rewritten when the document or its location changed, a failure is on
-- record, or the last good read is older than a day. jsonb compares by
-- value, so whitespace and key order in the upstream body do not count as a
-- change. Zero rows means the row already held this read.
INSERT INTO remote_protected_resources (
    project_id,
    organization_id,
    resource_identifier,
    metadata_url,
    authorization_servers,
    scopes_supported,
    bearer_methods_supported,
    resource_name,
    resource_documentation,
    resource_policy_uri,
    resource_tos_uri,
    dpop_bound_access_tokens_required,
    dpop_signing_alg_values_supported,
    tls_client_certificate_bound_access_tokens,
    metadata,
    metadata_fetched_at
)
VALUES (
    @project_id,
    @organization_id,
    @resource_identifier::text,
    NULLIF(@metadata_url::text, ''),
    sqlc.narg(authorization_servers)::text[],
    sqlc.narg(scopes_supported)::text[],
    sqlc.narg(bearer_methods_supported)::text[],
    NULLIF(@resource_name::text, ''),
    NULLIF(@resource_documentation::text, ''),
    NULLIF(@resource_policy_uri::text, ''),
    NULLIF(@resource_tos_uri::text, ''),
    sqlc.narg(dpop_bound_access_tokens_required)::boolean,
    sqlc.narg(dpop_signing_alg_values_supported)::text[],
    sqlc.narg(tls_client_certificate_bound_access_tokens)::boolean,
    NULLIF(@metadata::text, '')::jsonb,
    clock_timestamp()
)
ON CONFLICT (project_id, resource_identifier) WHERE deleted IS FALSE DO UPDATE
SET
    organization_id = EXCLUDED.organization_id,
    metadata_url = EXCLUDED.metadata_url,
    authorization_servers = EXCLUDED.authorization_servers,
    scopes_supported = EXCLUDED.scopes_supported,
    bearer_methods_supported = EXCLUDED.bearer_methods_supported,
    resource_name = EXCLUDED.resource_name,
    resource_documentation = EXCLUDED.resource_documentation,
    resource_policy_uri = EXCLUDED.resource_policy_uri,
    resource_tos_uri = EXCLUDED.resource_tos_uri,
    dpop_bound_access_tokens_required = EXCLUDED.dpop_bound_access_tokens_required,
    dpop_signing_alg_values_supported = EXCLUDED.dpop_signing_alg_values_supported,
    tls_client_certificate_bound_access_tokens = EXCLUDED.tls_client_certificate_bound_access_tokens,
    metadata = EXCLUDED.metadata,
    metadata_fetched_at = clock_timestamp(),
    metadata_last_error = NULL,
    metadata_last_error_at = NULL,
    updated_at = clock_timestamp()
WHERE remote_protected_resources.metadata IS DISTINCT FROM EXCLUDED.metadata
   OR remote_protected_resources.metadata_url IS DISTINCT FROM EXCLUDED.metadata_url
   OR remote_protected_resources.metadata_last_error_at IS NOT NULL
   OR remote_protected_resources.metadata_fetched_at IS NULL
   OR remote_protected_resources.metadata_fetched_at < clock_timestamp() - INTERVAL '24 hours';

-- name: RecordRemoteProtectedResourceFetchError :one
-- Records why the last read of resource_identifier failed without touching
-- what an earlier read advertised. Creates the row when none exists.
INSERT INTO remote_protected_resources (
    project_id,
    organization_id,
    resource_identifier,
    metadata_url,
    metadata_last_error,
    metadata_last_error_at
)
VALUES (
    @project_id,
    @organization_id,
    @resource_identifier::text,
    NULLIF(@metadata_url::text, ''),
    @metadata_last_error::text,
    clock_timestamp()
)
ON CONFLICT (project_id, resource_identifier) WHERE deleted IS FALSE DO UPDATE
SET
    metadata_url = COALESCE(EXCLUDED.metadata_url, remote_protected_resources.metadata_url),
    metadata_last_error = EXCLUDED.metadata_last_error,
    metadata_last_error_at = clock_timestamp(),
    updated_at = clock_timestamp()
RETURNING *;

-- name: RecordRemoteProtectedResourceChallengeScopes :execrows
UPDATE remote_protected_resources
SET
    challenge_scopes = @challenge_scopes::text[],
    challenge_scopes_seen_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
    AND resource_identifier = @resource_identifier::text
    AND deleted IS FALSE;

-- name: AcquireRemoteProtectedResourceLock :exec
-- Serializes writes to one resource row even before it exists, until commit or rollback.
SELECT pg_advisory_xact_lock(hashtextextended('remote_protected_resources:' || CAST(@project_id::uuid AS text) || ':' || @resource_identifier::text, 0));

-- name: GetRemoteURLForMcpServerForShare :one
-- GetRemoteURLForMcpServer holding the remote server row, so a URL edit waits for the scope pin write.
SELECT rms.url
FROM mcp_servers AS m
JOIN remote_mcp_servers AS rms ON rms.id = m.remote_mcp_server_id AND rms.project_id = m.project_id AND rms.deleted IS FALSE
WHERE m.id = @mcp_server_id
  AND m.project_id = @project_id
  AND m.deleted IS FALSE
FOR SHARE OF rms;

-- name: ListMcpServerIDsByRemoteURL :many
-- Live MCP servers in the project proxying to url, which share one protected resource.
SELECT m.id
FROM mcp_servers AS m
JOIN remote_mcp_servers AS rms ON rms.id = m.remote_mcp_server_id AND rms.project_id = m.project_id AND rms.deleted IS FALSE
WHERE m.project_id = @project_id
    AND rms.url = @url::text
    AND m.deleted IS FALSE
ORDER BY m.id;

-- name: UpsertRemoteProtectedResourceScopeOverride :one
-- Pins the scopes logins to this resource request, creating the row when the
-- resource was never read; NULL or an empty array clears the pin.
INSERT INTO remote_protected_resources (
    project_id,
    organization_id,
    resource_identifier,
    scope_override
)
VALUES (
    @project_id,
    @organization_id,
    @resource_identifier::text,
    CASE WHEN cardinality(sqlc.narg(scope_override)::text[]) > 0 THEN sqlc.narg(scope_override)::text[] END
)
ON CONFLICT (project_id, resource_identifier) WHERE deleted IS FALSE DO UPDATE
SET
    scope_override = EXCLUDED.scope_override,
    updated_at = clock_timestamp()
RETURNING *;

-- name: SetRemoteProtectedResourceMetadataTimestamps :execrows
-- Test fixture: backdates when the row was last read or last failed.
UPDATE remote_protected_resources
SET
    metadata_fetched_at = sqlc.narg(metadata_fetched_at)::timestamptz,
    metadata_last_error_at = sqlc.narg(metadata_last_error_at)::timestamptz
WHERE project_id = @project_id
    AND resource_identifier = @resource_identifier::text
    AND deleted IS FALSE;

-- name: GetRemoteProtectedResource :one
SELECT *
FROM remote_protected_resources
WHERE project_id = @project_id
    AND resource_identifier = @resource_identifier::text
    AND deleted IS FALSE;
