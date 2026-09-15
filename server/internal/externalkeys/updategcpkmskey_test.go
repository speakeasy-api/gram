package externalkeys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
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

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   credID,
		Name:                   "after",
		CustomerGrantReference: new("gram-signer@gram.iam.gserviceaccount.com"),
	})
	require.NoError(t, err)

	require.Equal(t, key.ID, updated.ID)
	require.Equal(t, "after", updated.Name)
	require.NotNil(t, updated.CustomerGrantReference)
	require.Equal(t, "gram-signer@gram.iam.gserviceaccount.com", *updated.CustomerGrantReference)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// TestUpdateGcpKmsKey_WrongFamilyCredential verifies the update rejects a swap
// to an aws_iam credential.
func TestUpdateGcpKmsKey_WrongFamilyCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "gcp-cred")
	key := createGcpKmsKey(t, ctx, ti, "key", credID)
	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")

	_, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   awsCredID,
		Name:                   "key",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestUpdateGcpKmsKey_WrongProvider verifies an aws_kms key id cannot be updated
// through the gcp_kms endpoint.
func TestUpdateGcpKmsKey_WrongProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")
	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	awsKey := createAwsKmsKey(t, ctx, ti, "aws-key", awsCredID)

	_, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     awsKey.ID,
		SessionToken:           nil,
		ExternalCredentialID:   gcpCredID,
		Name:                   "hijack",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestUpdateGcpKmsKey_IdentityIsImmutable verifies an update cannot re-point the
// row at a different crypto key version: the resource name and algorithm are set
// at creation and are not part of the update payload, so they survive an update
// untouched. A published JWK pins its kid to this row, so re-pointing it would
// silently sign with the wrong key.
func TestUpdateGcpKmsKey_IdentityIsImmutable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	key := createGcpKmsKey(t, ctx, ti, "before", credID)

	updated, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   credID,
		Name:                   "after",
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)

	require.Equal(t, key.ResourceName, updated.ResourceName)
	require.Equal(t, key.Algorithm, updated.Algorithm)

	// Re-read so the assertion covers what was persisted, not just the view the
	// update handler assembled.
	got, err := ti.service.GetGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpKmsKeyPayload{
		ID:           key.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, key.ResourceName, got.ResourceName)
	require.Equal(t, key.Algorithm, got.Algorithm)
	require.Equal(t, "after", got.Name)
}

// TestUpdateGcpKmsKey_SwapCredential verifies the backing credential can be
// replaced with another same-family credential. The path to the key stays
// editable even though the key material is frozen.
func TestUpdateGcpKmsKey_SwapCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "cred-a")
	key := createGcpKmsKey(t, ctx, ti, "key", credID)
	otherCredID := createGcpIamCredential(t, ctx, ti, "cred-b")

	updated, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     key.ID,
		SessionToken:           nil,
		ExternalCredentialID:   otherCredID,
		Name:                   "key",
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)
	require.Equal(t, otherCredID, updated.ExternalCredentialID)
}

func TestUpdateGcpKmsKey_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")

	_, err := ti.service.UpdateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpKmsKeyPayload{
		ID:                     uuid.NewString(),
		SessionToken:           nil,
		ExternalCredentialID:   credID,
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
		ExternalCredentialID:   credID,
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
		ExternalCredentialID:   credentialID,
		Name:                   "gcp-key-entitlement-update-renamed",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
