package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
)

func TestListExternalCredentials_ReturnsAllProviders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	aws := createAWSExternalIDCredential(t, ctx, ti, "aws-list")
	gcp := createGCPImpersonationCredential(t, ctx, ti, "gcp-list")

	result, err := ti.service.ListExternalCredentials(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)

	ids := credentialIDs(result)
	require.Contains(t, ids, aws.ID)
	require.Contains(t, ids, gcp.ID)
}

func TestListExternalCredentials_ProviderFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	aws := createAWSExternalIDCredential(t, ctx, ti, "aws-filter")
	gcp := createGCPImpersonationCredential(t, ctx, ti, "gcp-filter")

	result, err := ti.service.ListExternalCredentials(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListExternalCredentialsPayload{
		Provider:     new("aws_iam"),
		SessionToken: nil,
	})
	require.NoError(t, err)

	ids := credentialIDs(result)
	require.Contains(t, ids, aws.ID)
	require.NotContains(t, ids, gcp.ID)

	for _, c := range result.Credentials {
		require.Equal(t, "aws_iam", c.Provider)
	}
}

func TestListExternalCredentials_ExcludesDeleted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	kept := createAWSExternalIDCredential(t, ctx, ti, "aws-kept")
	removed := createAWSExternalIDCredential(t, ctx, ti, "aws-removed")

	err := ti.service.DeleteAwsIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.DeleteAwsIamCredentialPayload{
		ID:           removed.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	result, err := ti.service.ListExternalCredentials(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)

	ids := credentialIDs(result)
	require.Contains(t, ids, kept.ID)
	require.NotContains(t, ids, removed.ID)
}
