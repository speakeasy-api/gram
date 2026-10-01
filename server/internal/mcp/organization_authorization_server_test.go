package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// organizationGrantFixture is the workload grant fixture with the organization
// token endpoint switched on, and a second server the workload's agent may
// also reach.
type organizationGrantFixture struct {
	workloadGrantFixture

	orgSlug string
	// issuer is the organization authorization server's issuer on the
	// platform host, where it is served when no authentication host is.
	issuer string

	serverB   toolsetsrepo.Toolset
	resourceB string
}

func newOrganizationGrantFixture(t *testing.T) organizationGrantFixture {
	t.Helper()

	f := newWorkloadGrantFixture(t)
	f.ti.features.SetFlag(feature.FlagOrgTokenEndpoint, f.fx.orgID, true)

	organization, err := orgsrepo.New(f.ti.conn).GetOrganizationMetadata(f.ctx, f.fx.orgID)
	require.NoError(t, err)

	serverB, _, _ := seedPrivateToolsetWithIssuer(t, f.ctx, f.ti)
	seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), serverB.ID)

	return organizationGrantFixture{
		workloadGrantFixture: f,
		orgSlug:              organization.Slug,
		issuer:               platformBaseURL(f.ti) + "/o/" + organization.Slug,
		serverB:              serverB,
		resourceB:            platformBaseURL(f.ti) + "/mcp/" + serverB.McpSlug.String,
	}
}

func platformBaseURL(ti *testInstance) string {
	return strings.TrimSuffix(ti.serverURL.String(), "/")
}

// organizationRequest addresses the organization routes on the platform host,
// as the main mux dispatches them.
func organizationRequest(t *testing.T, method, path, orgSlug string, form url.Values) *http.Request {
	t.Helper()

	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("orgSlug", orgSlug)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func workloadGrantForm(assertion string, resources ...string) url.Values {
	form := url.Values{}
	form.Set("grant_type", workloadGrantJWTBearer)
	form.Set("assertion", assertion)
	for _, resource := range resources {
		form.Add("resource", resource)
	}
	return form
}

// exchangeAtOrganization posts the workload grant to the organization token
// endpoint on the platform host.
func (f organizationGrantFixture) exchangeAtOrganization(t *testing.T, assertion string, resources ...string) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	req := organizationRequest(t, http.MethodPost, "/o/"+f.orgSlug+"/token", f.orgSlug, workloadGrantForm(assertion, resources...))
	require.NoError(t, f.ti.service.HandleOrganizationToken(w, req))
	return w
}

// endpointFor resolves a server's endpoint as the MCP side resolves a request
// for it.
func (f organizationGrantFixture) endpointFor(t *testing.T, toolset toolsetsrepo.Toolset) *mcp.ResolvedMcpEndpoint {
	t.Helper()

	endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(f.ctx, f.ti.logger, toolset.McpSlug.String, "mcp")
	require.NoError(t, err)
	return endpoint
}

// requireOrganizationSession asserts an exchange minted a workload session
// issued by the organization for resource, and returns its access token.
func (f organizationGrantFixture) requireOrganizationSession(t *testing.T, w *httptest.ResponseRecorder, wantIssuer, resource string) string {
	t.Helper()

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotContains(t, resp, "refresh_token")

	claims := accessTokenClaims(t, w.Body.Bytes())
	require.Equal(t, wantIssuer, claims["iss"])
	require.Equal(t, urn.NewWorkloadSubject(f.issuerID, f.subject).String(), claims["sub"])
	require.Equal(t, []string{resource}, tokenAudiences(t, claims["aud"]))

	accessToken, ok := resp["access_token"].(string)
	require.True(t, ok)
	return accessToken
}

func tokenAudiences(t *testing.T, aud any) []string {
	t.Helper()

	switch v := aud.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			require.True(t, ok)
			out = append(out, s)
		}
		return out
	default:
		require.Failf(t, "unexpected aud claim", "%T", aud)
		return nil
	}
}

func requireTargetRefused(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.JSONEq(t, `{"error":"invalid_target","error_description":"resource is not available"}`, w.Body.String(),
		"every refused resource must read the same on the wire")
}

// One organization endpoint issues a session for each of two servers, and each
// session is bound to its own server alone.
func TestOrganizationToken_ExchangesForEachServerSeparately(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)
	endpointA := f.endpointFor(t, f.fx.toolset)
	endpointB := f.endpointFor(t, f.serverB)

	tokenA := f.requireOrganizationSession(t, f.exchangeAtOrganization(t, f.assertion(t, f.issuer), f.resource), f.issuer, f.resource)
	tokenB := f.requireOrganizationSession(t, f.exchangeAtOrganization(t, f.assertion(t, f.issuer+"/token"), f.resourceB), f.issuer, f.resourceB)

	for _, tc := range []struct {
		token    string
		accepted *mcp.ResolvedMcpEndpoint
		refused  *mcp.ResolvedMcpEndpoint
	}{
		{token: tokenA, accepted: endpointA, refused: endpointB},
		{token: tokenB, accepted: endpointB, refused: endpointA},
	} {
		_, _, _, err := f.ti.service.ApplyIssuerGate(f.ctx, httptest.NewRecorder(), tc.token, f.ti.serverURL.String(), tc.accepted)
		require.NoError(t, err, "the server the session was minted for must admit it")
		_, _, _, err = f.ti.service.ApplyIssuerGate(f.ctx, httptest.NewRecorder(), tc.token, f.ti.serverURL.String(), tc.refused)
		require.Error(t, err, "no other server may admit it")
	}
}

// A resource that does not exist, one in another organization, and one the
// workload's agent may not reach are refused alike.
func TestOrganizationToken_RefusesResourcesIndistinguishably(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)

	unreachable, _, _ := seedPrivateToolsetWithIssuer(t, f.ctx, f.ti)
	otherOrganization := seedToolsetInAnotherOrganization(t, f)

	for name, resource := range map[string]string{
		"unknown":            platformBaseURL(f.ti) + "/mcp/does-not-exist-" + uuid.NewString()[:8],
		"other organization": platformBaseURL(f.ti) + "/mcp/" + otherOrganization.McpSlug.String,
		"out of reach":       platformBaseURL(f.ti) + "/mcp/" + unreachable.McpSlug.String,
		"another host":       "https://elsewhere.example.com/mcp/" + f.fx.toolset.McpSlug.String,
		"not canonical":      f.resource + "/",
	} {
		w := f.exchangeAtOrganization(t, f.assertion(t, f.issuer), resource)
		requireTargetRefused(t, w)
		require.NotContains(t, w.Body.String(), resource, name)
	}
	require.Empty(t, f.workloadSessions(t))
}

// A server reachable only through the organization's IP-allowlisted custom
// domain is refused like any other unavailable resource.
func TestOrganizationToken_CustomDomainLockdownRefused(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)
	_, err := customdomainsrepo.New(f.ti.conn).CreateCustomDomain(f.ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: f.fx.orgID,
		Domain:         "org-token-lockdown-" + uuid.NewString()[:8] + ".example.com",
		IpAllowlist:    []string{"203.0.113.0/24"},
	})
	require.NoError(t, err)

	requireTargetRefused(t, f.exchangeAtOrganization(t, f.assertion(t, f.issuer), f.resource))
	require.Empty(t, f.workloadSessions(t))
}

func seedToolsetInAnotherOrganization(t *testing.T, f organizationGrantFixture) toolsetsrepo.Toolset {
	t.Helper()

	organizationID := "org_" + uuid.NewString()
	require.NoError(t, orgsrepo.New(f.ti.conn).CreateOrganizationMetadata(f.ctx, orgsrepo.CreateOrganizationMetadataParams{
		ID: organizationID, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8],
	}))
	slug := "other-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(f.ti.conn).CreateProject(f.ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	other := *authCtx
	other.ActiveOrganizationID = organizationID
	other.ProjectID = &project.ID
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, contextvalues.SetAuthContext(f.ctx, &other), f.ti)
	return toolset
}

// The assertion must name the organization's issuer or token endpoint exactly.
func TestOrganizationToken_AudienceMustBeTheOrganizationPair(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)

	for _, aud := range []string{
		f.advertisedIssuer,
		f.advertisedIssuer + "/token",
		platformBaseURL(f.ti),
		platformBaseURL(f.ti) + "/o/" + f.orgSlug + "/revoke",
		f.issuer + "/",
	} {
		requireWorkloadGrantRefused(t, f.exchangeAtOrganization(t, f.assertion(t, aud), f.resource))
	}
	require.Empty(t, f.workloadSessions(t))
}

func TestOrganizationToken_ExactlyOneResource(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)

	w := f.exchangeAtOrganization(t, f.assertion(t, f.issuer), f.resource, f.resourceB)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_target")

	w = f.exchangeAtOrganization(t, f.assertion(t, f.issuer))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_request")
}

func TestOrganizationToken_RefusesClientAuthenticationAndOtherGrants(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)

	form := workloadGrantForm(f.assertion(t, f.issuer), f.resource)
	form.Set("client_id", "some-client")
	w := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleOrganizationToken(w, organizationRequest(t, http.MethodPost, "/o/"+f.orgSlug+"/token", f.orgSlug, form)))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_request")

	form = url.Values{"grant_type": {"authorization_code"}, "code": {"x"}}
	w = httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleOrganizationToken(w, organizationRequest(t, http.MethodPost, "/o/"+f.orgSlug+"/token", f.orgSlug, form)))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "unsupported_grant_type")
}

func TestOrganizationToken_ReplayRefused(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)
	assertion := f.assertion(t, f.issuer)

	f.requireOrganizationSession(t, f.exchangeAtOrganization(t, assertion, f.resource), f.issuer, f.resource)
	requireWorkloadGrantRefused(t, f.exchangeAtOrganization(t, assertion, f.resource))
	requireWorkloadGrantRefused(t, f.exchangeAtOrganization(t, assertion, f.resourceB))
}

// An assertion is good at one authorization server only: spent at the
// organization endpoint it is not accepted at the server's own, and the
// reverse.
func TestOrganizationToken_AssertionsDoNotCrossAuthorizationServers(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)

	forOrganization := f.assertion(t, f.issuer)
	f.requireOrganizationSession(t, f.exchangeAtOrganization(t, forOrganization, f.resource), f.issuer, f.resource)
	requireWorkloadGrantRefused(t, f.exchange(t, forOrganization, f.resource))

	forServer := f.assertion(t, f.advertisedIssuer)
	f.requireWorkloadSession(t, f.exchange(t, forServer, f.resource), f.advertisedIssuer)
	requireWorkloadGrantRefused(t, f.exchangeAtOrganization(t, forServer, f.resource))

	unspent := f.assertion(t, f.issuer)
	requireWorkloadGrantRefused(t, f.exchange(t, unspent, f.resource))
}

func TestOrganizationToken_FlagOffIsNotFound(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)
	f.ti.features.SetFlag(feature.FlagOrgTokenEndpoint, f.fx.orgID, false)

	err := f.ti.service.HandleOrganizationToken(httptest.NewRecorder(), organizationRequest(t, http.MethodPost, "/o/"+f.orgSlug+"/token", f.orgSlug, workloadGrantForm(f.assertion(t, f.issuer), f.resource)))
	requireNotFound(t, err)

	err = f.ti.service.HandleGetOrganizationAuthorizationServer(httptest.NewRecorder(), organizationRequest(t, http.MethodGet, "/.well-known/oauth-authorization-server/o/"+f.orgSlug, f.orgSlug, nil))
	requireNotFound(t, err)

	server, err := f.ti.service.OrganizationAuthorizationServer(f.ctx, f.fx.orgID)
	require.NoError(t, err)
	require.False(t, server.Enabled)
	require.Equal(t, mcp.FederationOrganizationEndpointDisabled, server.NotReady)
	require.Empty(t, server.GrantTypesSupported)
}

func TestOrganizationToken_UnknownOrganizationIsNotFound(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)
	slug := "missing-" + uuid.NewString()[:8]

	err := f.ti.service.HandleGetOrganizationAuthorizationServer(httptest.NewRecorder(), organizationRequest(t, http.MethodGet, "/.well-known/oauth-authorization-server/o/"+slug, slug, nil))
	requireNotFound(t, err)
}

func fetchOrganizationMetadata(t *testing.T, f organizationGrantFixture) map[string]any {
	t.Helper()

	w := httptest.NewRecorder()
	require.NoError(t, f.ti.service.HandleGetOrganizationAuthorizationServer(w, organizationRequest(t, http.MethodGet, "/.well-known/oauth-authorization-server/o/"+f.orgSlug, f.orgSlug, nil)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	return meta
}

// The metadata document and what the management API reports come from one
// derivation, so they cannot disagree.
func TestOrganizationAuthorizationServer_MetadataMatchesTheDerivation(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)

	server, err := f.ti.service.OrganizationAuthorizationServer(f.ctx, f.fx.orgID)
	require.NoError(t, err)
	meta := fetchOrganizationMetadata(t, f)

	require.Equal(t, server.Issuer, meta["issuer"])
	require.Equal(t, server.TokenEndpoint, meta["token_endpoint"])
	require.Equal(t, []any{"urn:ietf:params:oauth:grant-type:jwt-bearer"}, meta["grant_types_supported"])
	require.Equal(t, []any{}, meta["response_types_supported"])
	require.Equal(t, []any{"none"}, meta["token_endpoint_auth_methods_supported"])
	require.NotContains(t, meta, "authorization_endpoint")
	require.NotContains(t, meta, "registration_endpoint")
	require.NotContains(t, meta, "revocation_endpoint")

	require.Equal(t, f.issuer, server.Issuer)
	require.Equal(t, f.issuer+"/token", server.TokenEndpoint)
	require.Equal(t, platformBaseURL(f.ti)+"/.well-known/oauth-authorization-server/o/"+f.orgSlug, server.MetadataURL)
	require.False(t, server.OnAuthenticationHost)
	require.True(t, server.Enabled)
	require.True(t, server.WorkloadGrantAdvertised)
	require.Equal(t, mcp.FederationReady, server.NotReady)
}

// With the agent authorization rollout off the endpoint still serves, but
// reports why an exchange cannot succeed and refuses it.
func TestOrganizationToken_AgentRolloutOffRefused(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)
	f.ti.features.SetFlag(feature.FlagAgentIdentityCredentials, f.fx.orgID, false)

	server, err := f.ti.service.OrganizationAuthorizationServer(f.ctx, f.fx.orgID)
	require.NoError(t, err)
	require.Equal(t, mcp.FederationAgentRolloutDisabled, server.NotReady)

	requireWorkloadGrantRefused(t, f.exchangeAtOrganization(t, f.assertion(t, f.issuer), f.resource))
	require.Empty(t, f.workloadSessions(t))
}

// With an authentication host configured the organization endpoint lives
// there alone, and its issuer names that host.
func TestOrganizationToken_ServedOnTheAuthenticationHost(t *testing.T) {
	t.Parallel()

	f := newOrganizationGrantFixture(t)
	harness := newAuthenticationHostHarness(t, f.ti)
	authIssuer := testAuthenticationHostURL + "/o/" + f.orgSlug

	server, err := f.ti.service.OrganizationAuthorizationServer(f.ctx, f.fx.orgID)
	require.NoError(t, err)
	require.Equal(t, authIssuer, server.Issuer)
	require.True(t, server.OnAuthenticationHost)

	w := harness.serve(t, http.MethodGet, "auth.example.com", "/.well-known/oauth-authorization-server/o/"+f.orgSlug, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	require.Equal(t, server.Issuer, meta["issuer"])
	require.Equal(t, server.TokenEndpoint, meta["token_endpoint"])

	w = harness.serve(t, http.MethodPost, "auth.example.com", "/o/"+f.orgSlug+"/token", workloadGrantForm(f.assertion(t, authIssuer+"/token"), f.resource))
	f.requireOrganizationSession(t, w, authIssuer, f.resource)

	// The platform host does not serve it, and the platform-host issuer is
	// not an accepted audience.
	err = f.ti.service.HandleOrganizationToken(httptest.NewRecorder(), organizationRequest(t, http.MethodPost, "/o/"+f.orgSlug+"/token", f.orgSlug, workloadGrantForm(f.assertion(t, f.issuer), f.resource)))
	requireNotFound(t, err)
	w = harness.serve(t, http.MethodPost, "auth.example.com", "/o/"+f.orgSlug+"/token", workloadGrantForm(f.assertion(t, f.issuer), f.resource))
	requireWorkloadGrantRefused(t, w)

	// Nothing else is served under the organization path.
	for _, path := range []string{"/o/" + f.orgSlug + "/authorize", "/o/" + f.orgSlug + "/register", "/o/" + f.orgSlug + "/revoke"} {
		w = harness.serve(t, http.MethodPost, "auth.example.com", path, url.Values{})
		require.Equal(t, http.StatusNotFound, w.Code, path)
	}
}
