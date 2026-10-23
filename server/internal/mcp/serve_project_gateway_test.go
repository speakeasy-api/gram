package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

func mintProjectGatewayBearer(t *testing.T, ti *testInstance, fx organizationGatewayFixture, projectSlug string) string {
	t.Helper()
	root := ti.serverURL.String() + "/mcp/gateway/" + fx.organizationSlug + "/projects/" + projectSlug
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: fx.subject, Audience: root, Issuer: root, Lifetime: time.Hour})
	require.NoError(t, err)
	persistTestUserSession(t, ti, fx.issuerID, fx.subject, jti)
	return token
}

func serveProjectGatewayHTTP(t *testing.T, ti *testInstance, fx organizationGatewayFixture, projectSlug, token string, body []byte) (*httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+fx.organizationSlug+"/projects/"+projectSlug, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rc := chi.NewRouteContext()
	rc.URLParams.Add("mcpSlug", fx.organizationSlug)
	rc.URLParams.Add("projectSlug", projectSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rc))
	w := httptest.NewRecorder()
	err := ti.service.ServeOrganizationGateway(w, req)
	return w, err
}

func TestProjectGateway_ProjectIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, other.ID, auth.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	otherSlug := "othermember-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, other.ID, meta.ID, "other public member", otherSlug, 0, mcpservers.VisibilityPublic)

	for _, tc := range []struct{ project, visible, excluded string }{
		{fx.projectSlug, fx.open, otherSlug},
		{other.Slug, otherSlug, fx.open},
	} {
		t.Run(tc.project, func(t *testing.T) {
			t.Parallel()
			token := mintProjectGatewayBearer(t, ti, fx, tc.project)
			w, err := serveProjectGatewayHTTP(t, ti, fx, tc.project, token, listServersBody(t))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"slug":"`+tc.visible+`"`)
			require.NotContains(t, w.Body.String(), tc.excluded)
			require.NotContains(t, w.Body.String(), fx.denied)
			if tc.project == fx.projectSlug {
				require.Contains(t, w.Body.String(), fx.granted)
			} else {
				require.NotContains(t, w.Body.String(), fx.granted)
			}
			w, err = serveProjectGatewayHTTP(t, ti, fx, tc.project, token, makeMetaRPCBody(t, "tools/call", map[string]any{
				"name": "execute_tool", "arguments": map[string]any{"name": tc.excluded + "--alpha", "arguments": map[string]any{}},
			}))
			require.NoError(t, err)
			require.Contains(t, w.Body.String(), "unknown server")
		})
	}
	// The organization-wide endpoint still sees both projects.
	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), fx.granted)
	require.Contains(t, w.Body.String(), fx.open)
	require.Contains(t, w.Body.String(), otherSlug)
	require.NotContains(t, w.Body.String(), fx.denied)

	projectToken := mintProjectGatewayBearer(t, ti, fx, fx.projectSlug)
	_, err = serveProjectGatewayHTTP(t, ti, fx, other.Slug, projectToken, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)
	_, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, projectToken, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)
	_, err = serveProjectGatewayHTTP(t, ti, fx, fx.projectSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestProjectGateway_ResolutionAndOAuthRoutes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := fx.organizationSlug + "/projects/" + fx.projectSlug
	path := "/mcp/gateway/" + slug
	root := ti.serverURL.String() + path
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, "mcp/gateway")
	require.NoError(t, err)
	require.Equal(t, *auth.ProjectID, endpoint.ProjectID)
	require.Equal(t, slug, endpoint.Slug)
	require.EqualValues(t, "mcp/gateway", endpoint.RouteBase)
	require.Equal(t, fx.issuerID, endpoint.UserSessionIssuerID)
	actualRoot, err := endpoint.RootURL(ti.serverURL.String())
	require.NoError(t, err)
	require.Equal(t, root, actualRoot)
	organizationEndpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), fx.organizationSlug, "mcp/gateway")
	require.NoError(t, err)
	require.NotEqual(t, organizationEndpoint.MetaMcpServerID, endpoint.MetaMcpServerID)
	ref, err := endpoint.EndpointRef(t.Context(), ti.conn, ti.serverURL.String())
	require.NoError(t, err)
	require.Equal(t, slug, ref.McpSlug)
	require.NoError(t, endpoint.ValidateRef(ref))
	require.Error(t, organizationEndpoint.ValidateRef(ref))
	reloaded, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, "mcp/gateway")
	require.NoError(t, err)
	require.Equal(t, endpoint.MetaMcpServerID, reloaded.MetaMcpServerID)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	getMetadata := func(prefix string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+path, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var metadata map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
		return metadata
	}
	protected := getMetadata("/.well-known/oauth-protected-resource")
	require.Equal(t, root, protected["resource"])
	metadata := getMetadata("/.well-known/oauth-authorization-server")
	require.Equal(t, root, metadata["issuer"])
	require.Equal(t, root+"/authorize", metadata["authorization_endpoint"])
	require.Equal(t, root+"/token", metadata["token_endpoint"])
	require.Equal(t, root+"/register", metadata["registration_endpoint"])
	w := postRegistration(t, router, path+"/register")
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	w, err = serveProjectGatewayHTTP(t, ti, fx, fx.projectSlug, "", listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource"+path)
	// Exercise the attached MCP route, not only the handler's path parameters.
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(listServersBody(t)))
	req.Header.Set("Authorization", "Bearer "+mintProjectGatewayBearer(t, ti, fx, fx.projectSlug))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), fx.granted)
}

func TestProjectGateway_MissingAndDeletedProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	deleted, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Deleted project", Slug: "deleted-" + uuid.NewString()[:8], OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, deleted.ID)
	require.NoError(t, err)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	for _, slug := range []string{"missing-" + uuid.NewString()[:8], deleted.Slug} {
		_, err := serveProjectGatewayHTTP(t, ti, fx, slug, "", makeInitializeBody())
		requireOopsCode(t, err, oops.CodeNotFound)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/gateway/"+fx.organizationSlug+"/projects/"+slug, nil))
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	}
	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestProjectGateway_ConsentChallengeResumesInProjectScope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, other.ID, auth.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	otherSlug := "othermember-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, other.ID, meta.ID, "other public member", otherSlug, 0, mcpservers.VisibilityPublic)
	slug := fx.organizationSlug + "/projects/" + fx.projectSlug
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, "mcp/gateway")
	require.NoError(t, err)
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(t.Context(), mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.issuerID,
		Endpoint: mcp.EndpointRef{
			McpSlug:         endpoint.Slug,
			RouteBase:       endpoint.RouteBase,
			MetaMcpServerID: endpoint.MetaMcpServerID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		RedirectURI:     "http://example.com/cb",
		CSRFToken:       "csrf-token",
		Subject:         &fx.subject,
		CreatedAt:       time.Now(),
		FirstParty:      true,
		AutoConnectDone: true,
	}))
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/mcp/gateway/"+slug+"/connect?state="+stateID, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `data-gateway-server="`+fx.granted+`"`)
	require.Contains(t, w.Body.String(), `data-gateway-server="`+fx.open+`"`)
	require.NotContains(t, w.Body.String(), fx.denied)
	require.NotContains(t, w.Body.String(), otherSlug)
}
