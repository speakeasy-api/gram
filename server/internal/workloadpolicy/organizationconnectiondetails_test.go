package workloadpolicy_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func organizationConnectionDetails(ctx context.Context, ti *testInstance) (*gen.WorkloadOrganizationConnectionDetails, error) {
	return ti.service.OrganizationConnectionDetails(ctx, &gen.OrganizationConnectionDetailsPayload{ //nolint:wrapcheck // tests assert on the handler's own error
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
}

func TestOrganizationConnectionDetails_RendersTheResolversAuthorizationServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.federation.organization = mcp.OrganizationAuthorizationServer{
		Issuer:                  "https://auth.example.com/o/acme",
		TokenEndpoint:           "https://auth.example.com/o/acme/token",
		MetadataURL:             "https://auth.example.com/.well-known/oauth-authorization-server/o/acme",
		OnAuthenticationHost:    true,
		Enabled:                 true,
		GrantTypesSupported:     []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		WorkloadGrantAdvertised: true,
		NotReady:                mcp.FederationReady,
	}

	orgCtx := authztest.WithExactGrants(t, withoutProject(t, ctx), authz.NewGrant(authz.ScopeWorkloadRead, ti.orgID))
	details, err := organizationConnectionDetails(orgCtx, ti)
	require.NoError(t, err)

	require.Equal(t, []string{ti.orgID}, ti.federation.organizations)
	require.True(t, details.Available)
	require.Equal(t, "https://auth.example.com/o/acme", details.Issuer)
	require.Equal(t, "https://auth.example.com/o/acme/token", details.TokenEndpoint)
	require.Equal(t, "https://auth.example.com/.well-known/oauth-authorization-server/o/acme", details.MetadataURL)
	require.True(t, details.OnAuthenticationHost)
	require.Equal(t, []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"}, details.GrantTypesSupported)
	require.True(t, details.WorkloadGrantAdvertised)
	require.True(t, details.Ready)
	require.Nil(t, details.NotReadyReason)
}

func TestOrganizationConnectionDetails_ReportsWhyItIsNotReady(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.federation.organization = mcp.OrganizationAuthorizationServer{
		Issuer:                  "https://app.example.com/o/acme",
		TokenEndpoint:           "https://app.example.com/o/acme/token",
		MetadataURL:             "https://app.example.com/.well-known/oauth-authorization-server/o/acme",
		OnAuthenticationHost:    false,
		Enabled:                 false,
		GrantTypesSupported:     []string{},
		WorkloadGrantAdvertised: false,
		NotReady:                mcp.FederationOrganizationEndpointDisabled,
	}

	details, err := organizationConnectionDetails(withoutProject(t, ctx), ti)
	require.NoError(t, err)
	require.False(t, details.Available)
	require.False(t, details.Ready)
	require.NotNil(t, details.NotReadyReason)
	require.Equal(t, string(mcp.FederationOrganizationEndpointDisabled), *details.NotReadyReason)
}

// The organization endpoint names no server, so a key scoped to one project
// reads the same organization values.
func TestOrganizationConnectionDetails_APIKeyReadsTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := organizationConnectionDetails(asAPIKey(t, ctx), ti)
	require.NoError(t, err)
	require.Equal(t, []string{ti.orgID}, ti.federation.organizations)
}

func TestOrganizationConnectionDetails_RequiresWorkloadRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	orgCtx := authztest.WithExactGrants(t, withoutProject(t, ctx), authz.NewGrant(authz.ScopeMCPRead, ti.orgID))
	_, err := organizationConnectionDetails(orgCtx, ti)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, ti.federation.organizations)
}
