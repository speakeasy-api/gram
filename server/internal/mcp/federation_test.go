package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
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

// createActivatedCustomDomain registers domainName to organizationID as a
// verified, activated custom domain.
func createActivatedCustomDomain(t *testing.T, ctx context.Context, ti *testInstance, organizationID, domainName string) customdomainsrepo.CustomDomain {
	t.Helper()

	domains := customdomainsrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: organizationID,
		Domain:         domainName,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
		IpAllowlist:    []string{},
	})
	require.NoError(t, err)
	domain, err = domains.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		ID:             domain.ID,
		Verified:       true,
		Activated:      true,
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return domain
}

// customDomainRequestContext is the context the custom-domain middleware
// gives a request that arrives on domain.
func customDomainRequestContext(ctx context.Context, organizationID string, domain customdomainsrepo.CustomDomain) context.Context {
	ctx = customdomains.WithContext(ctx, &customdomains.Context{
		OrganizationID: organizationID,
		Domain:         domain.Domain,
		DomainID:       domain.ID,
	})
	return requestorigin.WithContext(ctx, requestorigin.Origin{
		Surface:        requestorigin.SurfaceCustomDomain,
		BaseURL:        "https://" + domain.Domain,
		OrganizationID: organizationID,
	})
}

// servedDiscoveryDocument fetches a well-known document for mcpSlug through
// handler, as a request carrying ctx.
func servedDiscoveryDocument(t *testing.T, ctx context.Context, handler func(http.ResponseWriter, *http.Request) error, prefix, mcpSlug string) map[string]any {
	t.Helper()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, prefix+mcpSlug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, handler(w, req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var document map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &document))
	return document
}

// requireMatchesServedDocuments asserts that got carries the values the
// discovery documents served for mcpSlug to a request carrying ctx name.
func requireMatchesServedDocuments(t *testing.T, ctx context.Context, ti *testInstance, mcpSlug string, got mcp.FederationEndpoint) {
	t.Helper()

	protectedResource := servedDiscoveryDocument(t, ctx, ti.service.HandleGetProtectedResource, "/.well-known/oauth-protected-resource/mcp/", mcpSlug)
	authorizationServer := servedDiscoveryDocument(t, ctx, ti.service.HandleGetAuthorizationServer, "/.well-known/oauth-authorization-server/mcp/", mcpSlug)

	require.Equal(t, protectedResource["resource"], got.ResourceURL)
	require.Equal(t, authorizationServer["issuer"], got.Issuer)
	require.Equal(t, authorizationServer["token_endpoint"], got.TokenEndpoint)
	require.Equal(t, stringsOf(t, authorizationServer["grant_types_supported"]), got.GrantTypesSupported)
}

// A domain-root address answers at the custom domain's root, but the ingress
// forwards the root and its discovery documents to the address's slug path,
// so what a platform must be configured with is the slug path's resource,
// issuer and token endpoint, which the token endpoint requires exactly.
func TestFederationEndpoints_DomainRootMatchesTheServedDiscoveryDocuments(t *testing.T) {
	t.Parallel()

	f := newFederationFixture(t, mcpservers.VisibilityPrivate, true)
	ctx := t.Context()
	domain := createActivatedCustomDomain(t, ctx, f.ti, f.orgID, "root-"+uuid.NewString()[:8]+".example.com")
	rootSlug := "root-" + uuid.NewString()[:8]
	rootEndpoint, err := mcpendpointsrepo.New(f.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       f.server.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID:     uuid.NullUUID{UUID: f.server.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{},
		Slug:            rootSlug,
	})
	require.NoError(t, err)
	require.NoError(t, customdomainsrepo.New(f.ti.conn).SetRootMcpEndpoint(ctx, customdomainsrepo.SetRootMcpEndpointParams{
		McpEndpointID:  rootEndpoint.ID,
		CustomDomainID: domain.ID,
	}))

	endpoints, err := f.ti.service.FederationEndpoints(ctx, f.orgID, &f.server)
	require.NoError(t, err)
	require.Len(t, endpoints, 2)

	root := endpoints[1]
	require.Equal(t, "https://"+domain.Domain+"/mcp/"+rootSlug, root.ResourceURL)
	require.Equal(t, mcp.FederationReady, root.NotReady)
	requireMatchesServedDocuments(t, customDomainRequestContext(ctx, f.orgID, domain), f.ti, rootSlug, root)
}

// legacyToolsetServer is an issuer-gated toolset served through its MCP slug,
// wrapped by an MCP server that has no endpoint rows, in an organization under
// the agent authorization rollout.
func legacyToolsetServer(t *testing.T) (*testInstance, string, string, mcpserversrepo.McpServer) {
	t.Helper()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.features.SetFlag(feature.FlagAgentManagement, authCtx.ActiveOrganizationID, true)
	ti.features.SetFlag(feature.FlagAgentIdentityCredentials, authCtx.ActiveOrganizationID, true)

	slug := "legacy-" + uuid.NewString()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  toolset.ID,
		ProjectID:           toolset.ProjectID,
		Name:                conv.ToPGText("legacy mcp server"),
		Slug:                conv.ToPGText("legacy-server-" + uuid.NewString()[:8]),
		EnvironmentID:       uuid.NullUUID{},
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{},
		ToolsetID:           uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility:          mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	return ti, authCtx.ActiveOrganizationID, slug, server
}

// A server that predates its endpoint rows still answers on its toolset's
// MCP slug, so that address is listed with the values its served discovery
// documents carry.
func TestFederationEndpoints_LegacyToolsetAddressMatchesTheServedDiscoveryDocuments(t *testing.T) {
	t.Parallel()

	ti, orgID, slug, server := legacyToolsetServer(t)

	endpoints, err := ti.service.FederationEndpoints(t.Context(), orgID, &server)
	require.NoError(t, err)
	require.Len(t, endpoints, 1)

	got := endpoints[0]
	require.Equal(t, mcpHostRoot(ti, slug), got.ResourceURL)
	require.True(t, got.WorkloadGrantAdvertised)
	require.Equal(t, mcp.FederationReady, got.NotReady)
	requireMatchesServedDocuments(t, t.Context(), ti, slug, got)
}

// Once an endpoint row claims the toolset's slug, the serving path answers it
// through that row, so the address is listed once.
func TestFederationEndpoints_LegacySlugClaimedByAnEndpointIsListedOnce(t *testing.T) {
	t.Parallel()

	ti, orgID, slug, server := legacyToolsetServer(t)
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(t.Context(), mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       server.ProjectID,
		CustomDomainID:  uuid.NullUUID{},
		McpServerID:     uuid.NullUUID{UUID: server.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{},
		Slug:            slug,
	})
	require.NoError(t, err)

	endpoints, err := ti.service.FederationEndpoints(t.Context(), orgID, &server)
	require.NoError(t, err)
	require.Len(t, endpoints, 1)
	require.Equal(t, mcpHostRoot(ti, slug), endpoints[0].ResourceURL)
	require.Equal(t, mcp.FederationReady, endpoints[0].NotReady)
}
