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

func TestGetGcpKmsKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	credID := createGcpIamCredential(t, ctx, ti, "backing-cred")
	created := createGcpKmsKey(t, ctx, ti, "readable", credID)

	got, err := ti.service.GetGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpKmsKeyPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "readable", got.Name)
	require.Equal(t, "gcp_kms", got.Provider)
	require.Equal(t, created.ResourceName, got.ResourceName)
}

func TestGetGcpKmsKey_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpKmsKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestGetGcpKmsKey_WrongProvider verifies an aws_kms key is not retrievable
// through the gcp_kms endpoint.
func TestGetGcpKmsKey_WrongProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	awsCredID := createAwsIamCredential(t, ctx, ti, "aws-cred")
	awsKey := createAwsKmsKey(t, ctx, ti, "aws-key", awsCredID)

	_, err := ti.service.GetGcpKmsKey(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpKmsKeyPayload{
		ID:           awsKey.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
