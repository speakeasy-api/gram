-- Seal statements leave updated_at alone: the credential itself did not
-- change, and network ingress reconciliation treats a new updated_at as a new
-- desired state. Each one only lands while the row still holds the
-- application-key ciphertext the backfill read, so a concurrent rotation or
-- disconnect wins.

-- name: ListOrganizationsWithUnsealedCredentials :many
SELECT organization_id FROM slack_directory_connections
WHERE credentials_encrypted IS NOT NULL AND credentials_org_encrypted IS NULL
UNION
SELECT organization_id FROM device_integration_configs
WHERE deleted IS FALSE AND credentials_org_encrypted IS NULL
UNION
SELECT organization_id FROM network_ingresses
WHERE deleted IS FALSE AND credentials_encrypted IS NOT NULL AND credentials_org_encrypted IS NULL
ORDER BY organization_id;

-- name: ListUnsealedSlackDirectoryCredentials :many
SELECT id, credentials_encrypted::text AS credentials_encrypted
FROM slack_directory_connections
WHERE organization_id = @organization_id
  AND credentials_encrypted IS NOT NULL
  AND credentials_org_encrypted IS NULL
ORDER BY id;

-- name: SealSlackDirectoryCredentials :execrows
UPDATE slack_directory_connections
SET credentials_org_encrypted = @credentials_org_encrypted
WHERE organization_id = @organization_id
  AND id = @id
  AND credentials_encrypted = @credentials_encrypted
  AND credentials_org_encrypted IS NULL;

-- name: ListUnsealedDeviceIntegrationCredentials :many
SELECT id, credentials_encrypted
FROM device_integration_configs
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND credentials_org_encrypted IS NULL
ORDER BY id;

-- name: SealDeviceIntegrationCredentials :execrows
UPDATE device_integration_configs
SET credentials_org_encrypted = @credentials_org_encrypted
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND credentials_encrypted = @credentials_encrypted
  AND credentials_org_encrypted IS NULL;

-- name: ListUnsealedNetworkIngressCredentials :many
SELECT id, credentials_encrypted::text AS credentials_encrypted
FROM network_ingresses
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND credentials_encrypted IS NOT NULL
  AND credentials_org_encrypted IS NULL
ORDER BY id;

-- name: SealNetworkIngressCredentials :execrows
UPDATE network_ingresses
SET credentials_org_encrypted = @credentials_org_encrypted
WHERE organization_id = @organization_id
  AND id = @id
  AND deleted IS FALSE
  AND credentials_encrypted = @credentials_encrypted
  AND credentials_org_encrypted IS NULL;
