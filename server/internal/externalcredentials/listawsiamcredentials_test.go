package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
)

func TestListAwsIamCredentials_OnlyAws(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	aws := createAWSExternalIDCredential(t, ctx, ti, "aws-only")
	gcp := createGCPImpersonationCredential(t, ctx, ti, "gcp-excluded")

	result, err := ti.service.ListAwsIamCredentials(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListAwsIamCredentialsPayload{
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
