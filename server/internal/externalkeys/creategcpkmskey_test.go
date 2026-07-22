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
)

func TestCreateGcpKmsKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyCreate)
	require.NoError(t, err)

	key, err := ti.service.CreateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/1",
		ExternalCredentialID:   credID,
		Algorithm:              "ES256",
		Name:                   "signing-key",
		CustomerGrantReference: new("gram-signer@gram.iam.gserviceaccount.com"),
	})
	require.NoError(t, err)
	require.NotNil(t, key)

	require.Equal(t, "gcp_kms", key.Provider)
	require.Equal(t, "ES256", key.Algorithm)
	require.Equal(t, "signing-key", key.Name)
	require.Equal(t, "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/1", key.ResourceName)
	require.Equal(t, credID, key.ExternalCredentialID)
	require.NotNil(t, key.CustomerGrantReference)
	require.Equal(t, "gram-signer@gram.iam.gserviceaccount.com", *key.CustomerGrantReference)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionGcpKmsKeyCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestCreateGcpKmsKey_MissingResourceName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")

	_, err := ti.service.CreateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "   ",
		ExternalCredentialID:   credID,
		Algorithm:              "ES256",
		Name:                   "missing-resource",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestCreateGcpKmsKey_WrongFamilyCredential verifies a gcp_kms key cannot be
// backed by an aws_iam credential.
func TestCreateGcpKmsKey_WrongFamilyCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")

	_, err := ti.service.CreateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/1",
		ExternalCredentialID:   awsCredID,
		Algorithm:              "ES256",
		Name:                   "wrong-family",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateGcpKmsKey_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")

	_, err := ti.service.CreateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/1",
		ExternalCredentialID:   credID,
		Algorithm:              "ES256",
		Name:                   "forbidden",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateGcpKmsKey_WithoutGrantReference(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")

	key, err := ti.service.CreateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/no-grant/cryptoKeyVersions/1",
		ExternalCredentialID:   credID,
		Algorithm:              "RS256",
		Name:                   "no-grant-key",
		CustomerGrantReference: nil,
	})
	require.NoError(t, err)
	require.Nil(t, key.CustomerGrantReference)
}

func TestCreateGcpKmsKey_CredentialNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpKmsKeyPayload{
		SessionToken:           nil,
		ResourceName:           "projects/gram/locations/global/keyRings/signing/cryptoKeys/k/cryptoKeyVersions/1",
		ExternalCredentialID:   uuid.NewString(),
		Algorithm:              "ES256",
		Name:                   "missing-cred",
		CustomerGrantReference: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
