-- name: ListGlobalIDJAGIssuers :many
-- Global issuers whose authorization server advertises the jwt-bearer grant
-- and the ID-JAG grant profile, one row per global client (LEFT JOIN so an
-- issuer without a global client still exports, flagged as not ready).
-- Columns are named on purpose: never widen this to SELECT * or sqlc.embed,
-- which would pull client_secret_encrypted, jwks, and metadata into the
-- export type.
SELECT
  i.id                                     AS issuer_row_id,
  i.issuer                                 AS issuer,
  i.name                                   AS issuer_name,
  i.scopes_supported                       AS scopes_supported,
  i.grant_types_supported                  AS grant_types_supported,
  i.authorization_grant_profiles_supported AS grant_profiles_supported,
  i.client_id_metadata_document_supported  AS cimd_supported,
  i.metadata_fetched_at                    AS metadata_fetched_at,
  i.metadata_last_error                    AS metadata_last_error,
  c.id                                     AS client_row_id,
  c.client_id                              AS client_id,
  c.scope                                  AS client_scope,
  c.client_id_metadata_uri                 AS client_id_metadata_uri,
  c.resource_identifier                    AS resource_identifier,
  c.resource_name                          AS resource_name,
  c.token_endpoint_auth_method             AS token_endpoint_auth_method,
  c.upstream_rejected_at                   AS upstream_rejected_at
FROM remote_session_issuers i
LEFT JOIN remote_session_clients c
  ON c.remote_session_issuer_id = i.id
 AND c.project_id IS NULL AND c.organization_id IS NULL
 AND c.deleted IS FALSE
WHERE i.project_id IS NULL AND i.organization_id IS NULL
  AND i.deleted IS FALSE
  AND 'urn:ietf:params:oauth:grant-type:jwt-bearer' = ANY(i.grant_types_supported)
  AND 'urn:ietf:params:oauth:grant-profile:id-jag' = ANY(i.authorization_grant_profiles_supported)
ORDER BY i.issuer, i.id, c.id;
