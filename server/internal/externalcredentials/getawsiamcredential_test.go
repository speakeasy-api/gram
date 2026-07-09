package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetAwsIamCredential_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createAWSExternalIDCredential(t, ctx, ti, "aws-get")

	got, err := ti.service.GetAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.Equal(t, created.ID, got.ID)
	require.NotNil(t, got.ExternalID)
	require.Equal(t, *created.ExternalID, *got.ExternalID, "external_id must be readable so the customer can complete their trust policy")
}

func TestGetAwsIamCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsIamCredentialPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetAwsIamCredential_WrongProviderNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	gcp := createGCPImpersonationCredential(t, ctx, ti, "gcp-for-aws-get")

	_, err := ti.service.GetAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetAwsIamCredentialPayload{
		ID:           gcp.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
