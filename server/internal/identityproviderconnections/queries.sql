-- Serialize the entire create saga, including recovery and external provisioning.
-- name: LockIdentityProviderConnectionCreate :one
SELECT pg_try_advisory_xact_lock(hashtextextended('identity-provider-create:' || @organization_id::text, 0));

-- Every query here is organization-qualified. Rows the provisioner writes go
-- through the owning repos (externalkeys, jsonwebkeysets, remotesessions).

-- name: CreateIdentityProviderConnection :one
INSERT INTO identity_provider_connections (organization_id, provider)
VALUES (@organization_id, @provider)
RETURNING *;

-- name: GetIdentityProviderConnection :one
SELECT *
FROM identity_provider_connections
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE;

-- The revoke path reads through the tombstone so a deleted connection can
-- still withdraw its keys.
-- name: GetIdentityProviderConnectionIncludingDeleted :one
SELECT *
FROM identity_provider_connections
WHERE id = @id
  AND organization_id = @organization_id;

-- name: SoftDeleteIdentityProviderConnection :one
UPDATE identity_provider_connections
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Serializes provisioning and rotation per connection.
-- name: LockIdentityProviderConnectionForProvisioning :one
SELECT *
FROM identity_provider_connections
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
FOR UPDATE;

-- Organization-level only: a project-owned or platform issuer never carries a
-- managed client.
-- name: GetOrganizationRemoteSessionIssuerForProvisioning :one
SELECT *
FROM remote_session_issuers
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND deleted IS FALSE;

-- Speakeasy's own signing credential: platform-tier (organization_id IS NULL).
-- name: GetPlatformGcpIamCredentialForProvisioning :one
SELECT sqlc.embed(ec), sqlc.embed(gic)
FROM external_credentials AS ec
JOIN gcp_iam_credentials AS gic ON gic.external_credential_id = ec.id
WHERE ec.id = @id
  AND ec.organization_id IS NULL
  AND ec.project_id IS NULL
  AND ec.provider = 'gcp_iam'
  AND ec.deleted IS FALSE;

-- The managed client, its set, and the set's active key. The key join is LEFT
-- so a revoked connection (live client, live set, no live keys) still resolves.
-- Organization-level only: the client, its issuer, and the set all carry the
-- connection's organization scope, and the set must be marked by the same connection.
-- name: GetManagedClient :one
SELECT
  sqlc.embed(c),
  s.id AS json_web_key_set_id,
  s.external_key_id,
  k.id AS json_web_key_id,
  k.kid,
  k.activated_at
FROM remote_session_clients AS c
JOIN remote_session_issuers AS i
  ON i.id = c.remote_session_issuer_id
 AND i.organization_id = c.organization_id
 AND i.project_id IS NULL
 AND i.deleted IS FALSE
JOIN json_web_key_sets AS s
  ON s.organization_id = c.organization_id
 AND s.id = c.json_web_key_set_id
 AND s.identity_provider_connection_id = c.identity_provider_connection_id
 AND s.deleted IS FALSE
LEFT JOIN json_web_keys AS k
  ON k.organization_id = s.organization_id
 AND k.json_web_key_set_id = s.id
 AND k.state = 'active'
 AND k.deleted IS FALSE
WHERE c.organization_id = @organization_id
  AND c.project_id IS NULL
  AND c.identity_provider_connection_id = @identity_provider_connection_id
  AND c.deleted IS FALSE;

-- While pending, updated_at records observed publication (there is no published_at column).
-- A pending key is not timed until its publication commit has been observed.
-- These managed rows are organization-level (never project-owned).
-- name: MarkRotationPublicationUnobserved :exec
UPDATE json_web_keys SET updated_at = 'infinity'
WHERE id = @id AND organization_id = @organization_id AND state = 'pending' AND deleted IS FALSE;

-- Idempotent even when the publishing caller races a retry or revocation.
-- name: ObserveRotationPublication :one
UPDATE json_web_keys
SET updated_at = CASE WHEN updated_at = 'infinity' THEN clock_timestamp() ELSE updated_at END
WHERE id = @id AND organization_id = @organization_id AND state = 'pending' AND deleted IS FALSE
RETURNING updated_at;

-- Use the publication clock, not an application host's potentially skewed clock.
-- name: RotationPublicationReady :one
SELECT updated_at <= clock_timestamp() - make_interval(secs => @cache_seconds::integer)
FROM json_web_keys
WHERE id = @id AND organization_id = @organization_id AND state = 'pending' AND deleted IS FALSE;

-- Include tombstones and every organization: resource names are random and
-- global, so a reference anywhere means the key must never be disabled.
-- name: ManagedKeyResourceExists :one
SELECT EXISTS (
 SELECT 1 FROM gcp_kms_keys AS k
 WHERE k.resource_name = @resource_name
);

-- Test fixture: age a pending publication relative to the database clock.
-- name: BackdatePendingRotationPublication :exec
UPDATE json_web_keys
SET updated_at = clock_timestamp() - make_interval(secs => @age_seconds::integer)
WHERE json_web_key_set_id = @json_web_key_set_id
  AND organization_id = @organization_id AND state = 'pending' AND deleted IS FALSE;

-- Connection management API. Every query below is organization-qualified.

-- The live parent with its Okta details if any; a parent without them is a
-- create that failed midway and can be abandoned.
-- name: GetLiveOktaIdentityProviderConnectionForOrganization :one
SELECT
  sqlc.embed(c),
  o.identity_provider_connection_id AS okta_identity_provider_connection_id
FROM identity_provider_connections AS c
LEFT JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
 AND o.organization_id = c.organization_id
 AND o.deleted IS FALSE
WHERE c.organization_id = @organization_id
  AND c.provider = 'okta'
  AND c.deleted IS FALSE;

-- Durable creation cap: tombstones count, so revoke-and-recreate loops cannot
-- mint unbounded KMS keys.
-- name: CountIdentityProviderConnectionsCreatedSince :one
SELECT COUNT(*)
FROM identity_provider_connections
WHERE organization_id = @organization_id
  AND provider = @provider
  AND created_at >= @since;

-- The issuer a create minted for a connection, found by its deterministic slug
-- when the Okta details were never written.
-- name: GetConnectionIssuerBySlug :one
SELECT id
FROM remote_session_issuers
WHERE organization_id = @organization_id
  AND project_id IS NULL
  AND slug = @slug
  AND deleted IS FALSE;

-- name: CreateOktaIdentityProviderConnection :one
INSERT INTO okta_identity_provider_connections (
  identity_provider_connection_id,
  organization_id,
  org_url,
  issuer_url,
  ownership_claimed,
  remote_session_issuer_id,
  remote_session_client_id,
  listing_mode
)
VALUES (
  @identity_provider_connection_id,
  @organization_id,
  @org_url,
  @issuer_url,
  FALSE,
  @remote_session_issuer_id,
  @remote_session_client_id,
  @listing_mode
)
RETURNING *;

-- Live connection with its Okta details. Without an id, the organization's
-- single live Okta connection.
-- name: GetOktaIdentityProviderConnection :one
SELECT sqlc.embed(c), sqlc.embed(o)
FROM identity_provider_connections AS c
JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
 AND o.organization_id = c.organization_id
 AND o.deleted IS FALSE
WHERE c.organization_id = @organization_id
  AND c.provider = 'okta'
  AND (sqlc.narg('id')::uuid IS NULL OR c.id = sqlc.narg('id')::uuid)
  AND c.deleted IS FALSE;

-- The revoke path reads through the tombstone so a repeat is a no-op rather
-- than a not-found.
-- name: GetOktaIdentityProviderConnectionIncludingDeleted :one
SELECT sqlc.embed(c), sqlc.embed(o)
FROM identity_provider_connections AS c
JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
 AND o.organization_id = c.organization_id
WHERE c.id = @id
  AND c.organization_id = @organization_id
  AND c.provider = 'okta';

-- Serializes client id submission, verification, and revocation per connection.
-- name: LockOktaIdentityProviderConnection :one
SELECT sqlc.embed(c), sqlc.embed(o)
FROM identity_provider_connections AS c
JOIN okta_identity_provider_connections AS o
  ON o.identity_provider_connection_id = c.id
 AND o.organization_id = c.organization_id
 AND o.deleted IS FALSE
WHERE c.id = @id
  AND c.organization_id = @organization_id
  AND c.provider = 'okta'
  AND c.deleted IS FALSE
FOR UPDATE OF c;

-- name: UpdateIdentityProviderConnectionVerification :one
UPDATE identity_provider_connections
SET status = @status,
    last_verified_at = @last_verified_at,
    last_error = sqlc.narg('last_error'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- A failed verification is written outside the rolled-back attempt; the
-- compare-and-swap on updated_at keeps it from clobbering a concurrent run
-- that committed in between.
-- name: RecordIdentityProviderConnectionVerificationFailure :one
UPDATE identity_provider_connections
SET status = @status,
    last_error = @last_error,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND updated_at = @expected_updated_at
  AND deleted IS FALSE
RETURNING *;

-- name: UpdateOktaIdentityProviderConnectionVerification :one
UPDATE okta_identity_provider_connections
SET ownership_claimed = ownership_claimed OR @ownership_claimed::boolean,
    dpop_required = @dpop_required,
    granted_scopes = @granted_scopes,
    updated_at = clock_timestamp()
WHERE identity_provider_connection_id = @identity_provider_connection_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: UpdateOktaIdentityProviderConnectionAgent :one
UPDATE okta_identity_provider_connections
SET agent_id = sqlc.narg('agent_id'),
    agent_app_id = sqlc.narg('agent_app_id'),
    updated_at = clock_timestamp()
WHERE identity_provider_connection_id = @identity_provider_connection_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Revocation tombstones the connection so the organization can create a new
-- one; the managed client and set stay live serving an empty JWKS.
-- name: RevokeIdentityProviderConnection :one
UPDATE identity_provider_connections
SET status = 'revoked',
    last_error = NULL,
    deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: SoftDeleteOktaIdentityProviderConnection :one
UPDATE okta_identity_provider_connections
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE identity_provider_connection_id = @identity_provider_connection_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Only the provisioner writes a managed client's client_id, and only while the
-- provisioning placeholder is still in place.
-- name: SetManagedClientID :one
UPDATE remote_session_clients
SET client_id = @client_id,
    upstream_rejected_at = NULL,
    updated_at = clock_timestamp()
WHERE id = @id
  AND organization_id = @organization_id
  AND project_id IS NULL
  AND identity_provider_connection_id = @identity_provider_connection_id
  AND client_id = @placeholder_client_id
  AND deleted IS FALSE
RETURNING *;

-- Two live registrations of one client id against one issuer would share a
-- credential; refused in the write path since no index enforces it.
-- name: ManagedClientIDInUse :one
SELECT EXISTS (
  SELECT 1
  FROM remote_session_clients
  WHERE remote_session_issuer_id = @remote_session_issuer_id
    AND organization_id = @organization_id
    AND client_id = @client_id
    AND id <> @exclude_id
    AND deleted IS FALSE
);

-- A request newer than the watermark keeps the connection due even when a
-- run was in flight when it arrived.
-- name: RequestOktaApplicationsSync :one
UPDATE okta_identity_provider_connections
SET applications_sync_requested_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE identity_provider_connection_id = @identity_provider_connection_id
  AND organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- Reads the last applications sync, so it is only as fresh as that sync.
-- name: GetOktaAgentAppState :one
SELECT
  a.status,
  (
    SELECT count(*)
    FROM okta_application_assignments s
    WHERE s.organization_id = a.organization_id
      AND s.identity_provider_connection_id = a.identity_provider_connection_id
      AND s.okta_app_id = a.okta_app_id
      AND s.removed_at IS NULL
  ) AS assignment_count
FROM okta_applications a
WHERE a.organization_id = @organization_id
  AND a.identity_provider_connection_id = @identity_provider_connection_id
  AND a.okta_app_id = @okta_app_id
  AND a.removed_at IS NULL;

-- name: HasOktaResourceConnection :one
SELECT EXISTS (
  SELECT 1
  FROM okta_resource_connections
  WHERE organization_id = @organization_id
    AND identity_provider_connection_id = @identity_provider_connection_id
);
