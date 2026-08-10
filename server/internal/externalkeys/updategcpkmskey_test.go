package externalkeys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestUpdateGcpKmsKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "before", credID)

	updated, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/2",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "after",
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)

	require.Equal(t, key.ID, updated.ID)
	require.Equal(t, "after", updated.Name)
	require.Equal(t, "RS256", updated.Algorithm)
	require.Equal(t, "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/2", updated.ResourceName)
}

func TestUpdateGcpKmsKey_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")

	_, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     uuid.NewString(),
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/1",
		ExternalCredentialID:   credID,
		Algorithm:              "ES256",
		Name:                   "missing",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateGcpKmsKey_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "key", credID)

	_, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ResourceName:           key.ResourceName,
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "forbidden",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpdateGcpKmsKey_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credentialID := createGcpIamCredential(t, ctx, ti, "gcp-cred-entitlement-update")
	key := createGcpKmsKey(t, ctx, ti, "gcp-key-entitlement-update", credentialID)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	// A complete payload, so the assertion still proves the entitlement gate
	// rather than tripping over field validation if the two are ever reordered.
	_, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ResourceName:           key.ResourceName,
		ExternalCredentialID:   credentialID,
		Algorithm:              key.Algorithm,
		Name:                   "gcp-key-entitlement-update-renamed",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
