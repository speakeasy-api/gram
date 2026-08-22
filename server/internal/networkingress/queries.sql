-- name: CreateNetworkIngress :one
INSERT INTO network_ingresses (
    organization_id,
    provider,
    hostname,
    credential_kind,
    auth_key_enc,
    oauth_client_id,
    oauth_client_secret_enc,
    tags,
    private_network_only,
    identity_required
) VALUES (
    @organization_id,
    @provider,
    @hostname,
    @credential_kind,
    @auth_key_enc,
    @oauth_client_id,
    @oauth_client_secret_enc,
    @tags,
    @private_network_only,
    @identity_required
)
RETURNING *;

-- name: GetNetworkIngressByOrganization :one
SELECT *
FROM network_ingresses
WHERE organization_id = @organization_id
  AND deleted IS FALSE
LIMIT 1;

-- name: LockNetworkIngressByOrganization :one
SELECT *
FROM network_ingresses
WHERE organization_id = @organization_id
  AND deleted IS FALSE
LIMIT 1
FOR UPDATE;

-- name: UpdateNetworkIngressSettings :one
UPDATE network_ingresses
SET
    hostname = CASE WHEN @update_hostname::boolean THEN @hostname ELSE hostname END,
    enabled = CASE WHEN @update_enabled::boolean THEN @enabled ELSE enabled END,
    private_network_only = CASE WHEN @update_private_network_only::boolean THEN @private_network_only ELSE private_network_only END,
    identity_required = CASE WHEN @update_identity_required::boolean THEN @identity_required ELSE identity_required END,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: RotateNetworkIngressCredentials :one
-- Replacing the credential resets learned node health so the gateway
-- re-authenticates with the new material.
UPDATE network_ingresses
SET
    credential_kind = @credential_kind,
    auth_key_enc = @auth_key_enc,
    oauth_client_id = @oauth_client_id,
    oauth_client_secret_enc = @oauth_client_secret_enc,
    status = 'pending',
    last_error = NULL,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteNetworkIngress :exec
UPDATE network_ingresses
SET deleted_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND deleted IS FALSE;
