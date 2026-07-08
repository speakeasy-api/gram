package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
)

func TestListGcpIamCredentials_OnlyGcp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	aws := createAWSExternalIDCredential(t, ctx, ti, "aws-excluded")
	gcp := createGCPImpersonationCredential(t, ctx, ti, "gcp-only")

	result, err := ti.service.ListGcpIamCredentials(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListGcpIamCredentialsPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)

	ids := credentialIDs(result)
	require.Contains(t, ids, gcp.ID)
	require.NotContains(t, ids, aws.ID)

	for _, c := range result.Credentials {
		require.Equal(t, "gcp_iam", c.Provider)
	}
}
