package mcp_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// federationFixture is an issuer-gated, remote-backed MCP server with one
// platform endpoint, in an organization under the agent authorization rollout.
type federationFixture struct {
	ti       *testInstance
	orgID    string
	issuerID uuid.UUID
	server   mcpserversrepo.McpServer
	slug     string
}

func newFederationFixture(t *testing.T, visibility string, gated bool) federationFixture {
	t.Helper()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ti.features.SetFlag(feature.FlagAgentManagement, authCtx.ActiveOrganizationID, true)
	ti.features.SetFlag(feature.FlagAgentIdentityCredentials, authCtx.ActiveOrganizationID, true)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	gate := uuid.Nil
	if gated {
		gate = issuerID
	}
	slug := "federation-" + uuid.NewString()[:8]
	server, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, "https://upstream.example.com/mcp", slug, visibility, gate)

	return federationFixture{ti: ti, orgID: authCtx.ActiveOrganizationID, issuerID: issuerID, server: server, slug: slug}
}

func (f federationFixture) only(t *testing.T) mcp.FederationEndpoint {
	t.Helper()

	endpoints, err := f.ti.service.FederationEndpoints(t.Context(), f.orgID, &f.server)
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	return endpoints[0]
}

// servedAuthorizationServerMetadata fetches the RFC 8414 document from the
// host that serves it: the authentication host when the issuer opts in to
// it, otherwise the MCP host.
func servedAuthorizationServerMetadata(t *testing.T, ti *testInstance, harness *authenticationHostHarness, slug string) map[string]any {
	t.Helper()

	if harness == nil {
		return fetchASMetadata(t, ti, slug)
	}
	w := harness.serve(t, http.MethodGet, "auth.example.com", "/.well-known/oauth-authorization-server/mcp/"+slug, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	return meta
}

func stringsOf(t *testing.T, value any) []string {
	t.Helper()

	items, ok := value.([]any)
	require.True(t, ok, "expected a JSON array, got %T", value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		require.True(t, ok)
		out = append(out, s)
	}
	return out
}

// The values an operator is shown are the values a client discovers: the
// issuer, token endpoint and grant types equal the served authorization
// server metadata, and the resource equals the served protected-resource
// metadata, with and without the authentication host and the workload grant.
func TestFederationEndpoints_MatchTheServedDiscoveryDocuments(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		authenticationHost bool
		workloadGrant      bool
		wantNotReady       mcp.FederationNotReadyReason
	}{
		{name: "mcp host with the grant", authenticationHost: false, workloadGrant: true, wantNotReady: mcp.FederationReady},
		{name: "authentication host with the grant", authenticationHost: true, workloadGrant: true, wantNotReady: mcp.FederationReady},
		{name: "mcp host without the grant", authenticationHost: false, workloadGrant: false, wantNotReady: mcp.FederationWorkloadGrantUnavailable},
		{name: "authentication host without the grant", authenticationHost: true, workloadGrant: false, wantNotReady: mcp.FederationWorkloadGrantUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFederationFixture(t, mcpservers.VisibilityPrivate, true)
			var harness *authenticationHostHarness
			if tc.authenticationHost {
				h := newAuthenticationHostHarness(t, f.ti)
				harness = &h
				useAuthenticationHost(t, t.Context(), f.ti, f.orgID, f.issuerID)
			}
			if !tc.workloadGrant {
				f.ti.service.DisableWorkloadGrant()
			}

			got := f.only(t)
			served := servedAuthorizationServerMetadata(t, f.ti, harness, f.slug)
			servedGrants := stringsOf(t, served["grant_types_supported"])

			require.Equal(t, served["issuer"], got.Issuer)
			require.Equal(t, served["token_endpoint"], got.TokenEndpoint)
			require.Equal(t, servedGrants, got.GrantTypesSupported)
			require.Equal(t, fetchProtectedResourceMetadata(t, f.ti, f.slug)["resource"], got.ResourceURL)
			require.Equal(t, tc.authenticationHost, got.OnAuthenticationHost)
			require.Equal(t, tc.workloadGrant, got.WorkloadGrantAdvertised)
			require.Equal(t, tc.workloadGrant, slices.Contains(servedGrants, workloadGrantJWTBearer))
			require.Equal(t, tc.wantNotReady, got.NotReady)
			if tc.authenticationHost {
				require.Equal(t, authenticationHostTokenURL(f.slug), got.TokenEndpoint)
			}
		})
	}
}

// The token endpoint refuses every workload exchange while the organization is
// outside the agent authorization rollout, although the metadata still lists
// the grant, so the endpoint is reported as not ready.
func TestFederationEndpoints_RolloutOffIsNotReady(t *testing.T) {
	t.Parallel()

	f := newFederationFixture(t, mcpservers.VisibilityPrivate, true)
	f.ti.features.SetFlag(feature.FlagAgentIdentityCredentials, f.orgID, false)

	got := f.only(t)
	require.True(t, got.WorkloadGrantAdvertised)
	require.Equal(t, mcp.FederationAgentRolloutDisabled, got.NotReady)
	require.NotEmpty(t, got.TokenEndpoint)
}

// A server not gated on a Gram issuer has no Gram authorization server, so
// there is no token endpoint to show.
func TestFederationEndpoints_UngatedServerHasNoAuthorizationServer(t *testing.T) {
	t.Parallel()

	f := newFederationFixture(t, mcpservers.VisibilityPrivate, false)

	got := f.only(t)
	require.Equal(t, mcp.FederationNoAuthorizationServer, got.NotReady)
	require.Empty(t, got.Issuer)
	require.Empty(t, got.TokenEndpoint)
	require.Empty(t, got.GrantTypesSupported)
	require.Equal(t, mcpHostRoot(f.ti, f.slug), got.ResourceURL)
}

// A disabled server's address does not resolve publicly, so an exchange there
// cannot reach an authorization server at all.
func TestFederationEndpoints_DisabledServerIsNotReachable(t *testing.T) {
	t.Parallel()

	f := newFederationFixture(t, mcpservers.VisibilityDisabled, true)

	got := f.only(t)
	require.Equal(t, mcp.FederationNotPubliclyReachable, got.NotReady)
	require.Empty(t, got.TokenEndpoint)
}

func TestFederationEndpoints_ServerWithoutAnAddressHasNone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	server := mcpserversrepo.McpServer{ID: uuid.New(), ProjectID: *authCtx.ProjectID}
	endpoints, err := ti.service.FederationEndpoints(ctx, authCtx.ActiveOrganizationID, &server)
	require.NoError(t, err)
	require.Empty(t, endpoints)
}
