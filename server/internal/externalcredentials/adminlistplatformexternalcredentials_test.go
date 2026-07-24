package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
)

func platformCredentialIDs(result *adminecgen.ListExternalCredentialsResult) []string {
	ids := make([]string, len(result.Credentials))
	for i, c := range result.Credentials {
		ids[i] = c.ID
	}
	return ids
}

func TestListPlatformExternalCredentials_ReturnsPlatformCredentials(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	a := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-a")
	b := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-b")

	result, err := ti.service.ListPlatformExternalCredentials(withAdmin(t, ctx), &adminecgen.ListPlatformExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{a.ID, b.ID}, platformCredentialIDs(result))
	for _, c := range result.Credentials {
		require.Empty(t, c.OrganizationID, "platform list must only contain platform-scoped rows")
	}
}

func TestListPlatformExternalCredentials_ProviderFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-gcp")

	result, err := ti.service.ListPlatformExternalCredentials(withAdmin(t, ctx), &adminecgen.ListPlatformExternalCredentialsPayload{
		Provider:     new("gcp_iam"),
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{cred.ID}, platformCredentialIDs(result))

	awsOnly, err := ti.service.ListPlatformExternalCredentials(withAdmin(t, ctx), &adminecgen.ListPlatformExternalCredentialsPayload{
		Provider:     new("aws_iam"),
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Empty(t, awsOnly.Credentials)
}

// Platform and organization credential sets are disjoint: the platform list
// excludes org rows, and the org list excludes platform rows.
func TestListPlatformExternalCredentials_DisjointFromOrg(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	platformCred := createPlatformGCPAmbientCredential(t, ctx, ti, "platform-scoped")
	orgCred := createGCPImpersonationCredential(t, ctx, ti, "org-scoped")

	platformResult, err := ti.service.ListPlatformExternalCredentials(withAdmin(t, ctx), &adminecgen.ListPlatformExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{platformCred.ID}, platformCredentialIDs(platformResult))

	orgResult, err := ti.service.ListExternalCredentials(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.ListExternalCredentialsPayload{
		Provider:     nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{orgCred.ID}, credentialIDs(orgResult))
}
