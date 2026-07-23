package externalkeys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetAwsKmsKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createAwsIamCredential(t, ctx, ti, "backing-cred")
	created := createAwsKmsKey(t, ctx, ti, "readable", credID)

	got, err := ti.service.GetAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsKmsKeyPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "readable", got.Name)
	require.Equal(t, "aws_kms", got.Provider)
	require.Equal(t, created.KeyArn, got.KeyArn)
}

func TestGetAwsKmsKey_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestGetAwsKmsKey_WrongProvider verifies a gcp_kms key is not retrievable
// through the aws_kms endpoint.
func TestGetAwsKmsKey_WrongProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	gcpCredID := createGcpIamCredential(t, ctx, ti, "gcp-cred")
	gcpKey := createGcpKmsKey(t, ctx, ti, "gcp-key", gcpCredID)

	_, err := ti.service.GetAwsKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsKmsKeyPayload{
		ID:           gcpKey.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
