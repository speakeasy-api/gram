// implicit_issuer_test.go covers the implicit Gram-as-IdP surface
// (mcpservers.EligibleForImplicitIssuer): runtime and well-known paths
// stay read-only, OAuth entry points materialise the default issuer, and
// a JWT minted against it passes the serve gate.
package xmcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

// defaultIssuerMaterialised reports whether the project-default issuer's
// backing row exists at its deterministic id.
func defaultIssuerMaterialised(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) bool {
	t.Helper()
	_, err := usersessions_repo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
		ID:        usersessions.DefaultIssuerID(projectID),
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	require.NoError(t, err)
	return true
}

// TestServeMCP_ImplicitIssuer_UnauthChallenge asserts an unauthenticated
// request against a private remote server with no explicit issuer 401s with
// a WWW-Authenticate challenge pointing at the /x/mcp protected-resource
// metadata — the OAuth bootstrap MCP clients need — and that serving the
// challenge does NOT materialise the project-default issuer (the runtime
// path is read-only).
func TestServeMCP_ImplicitIssuer_UnauthChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, mockServer.URL, "private")

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, "", []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	wantChallenge := fmt.Sprintf(
		`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/x/mcp/%s"`,
		ti.serverURL.String(), slug,
	)
	require.Equal(t, wantChallenge, rr.Header().Get("WWW-Authenticate"))

	require.False(t, defaultIssuerMaterialised(t, ctx, ti, *authCtx.ProjectID),
		"serving a challenge must not materialise the default issuer")
}

// TestWellKnown_ImplicitIssuer_MetadataWithoutMaterialising asserts both
// well-known documents resolve for a private remote server with no explicit
// issuer — previously a 404 dead-end — and that the GETs stay read-only.
func TestWellKnown_ImplicitIssuer_MetadataWithoutMaterialising(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "private")

	root := ti.serverURL.String() + "/x/mcp/" + slug

	asW, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthServerMetadata, "/.well-known/oauth-authorization-server/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, asW.Code, "body=%s", asW.Body.String())
	var asDoc struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		RegistrationEndpoint  string `json:"registration_endpoint"`
	}
	require.NoError(t, json.Unmarshal(asW.Body.Bytes(), &asDoc))
	require.Equal(t, root, asDoc.Issuer)
	require.Equal(t, root+"/authorize", asDoc.AuthorizationEndpoint)
	require.Equal(t, root+"/register", asDoc.RegistrationEndpoint)

	prW, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, prW.Code, "body=%s", prW.Body.String())
	var prDoc struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	require.NoError(t, json.Unmarshal(prW.Body.Bytes(), &prDoc))
	require.Equal(t, root, prDoc.Resource)
	require.Equal(t, []string{root}, prDoc.AuthorizationServers)

	require.False(t, defaultIssuerMaterialised(t, ctx, ti, *authCtx.ProjectID),
		"well-known GETs must not materialise the default issuer")
}

// TestWellKnown_PublicRemoteBackend_StillNotFound pins the boundary of the
// implicit surface: a PUBLIC remote server with no issuer keeps 404ing from
// well-known — Gram is not its authorization server.
// (TestHandleWellKnownOAuthServerMetadata_RemoteBackend covers the AS
// document; this covers the protected-resource document.)
func TestWellKnown_PublicRemoteBackend_StillNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, "https://upstream.invalid/mcp", "public")

	w, err := runWellKnown(t, ctx, ti.service.HandleWellKnownOAuthProtectedResourceMetadata, "/.well-known/oauth-protected-resource/x/mcp/"+slug, slug)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no OAuth configuration found")
	require.Empty(t, w.Body.String())
}

// TestOAuthRegister_ImplicitIssuer_MaterialisesDefaultIssuer asserts DCR —
// the first stateful step of the OAuth flow — materialises the
// project-default issuer's backing row at its deterministic id.
func TestOAuthRegister_ImplicitIssuer_MaterialisesDefaultIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	slug, _, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, mockServer.URL, "private")

	mux := goahttp.NewMuxer()
	xmcp.Attach(mux, ti.service, nil)

	regBody := []byte(`{"client_name":"implicit dance","redirect_uris":["http://localhost:3000/callback"],"token_endpoint_auth_method":"none"}`)
	regReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/"+slug+"/register", bytes.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	mux.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code, "register; body=%s", regW.Body.String())

	issuer, err := usersessions_repo.New(ti.conn).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
		ID:        usersessions.DefaultIssuerID(*authCtx.ProjectID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err, "register must materialise the default issuer")
	require.Equal(t, *authCtx.ProjectID, issuer.ProjectID)
}

// TestServeMCP_ImplicitIssuer_JWTHappyPath asserts a user-session JWT
// minted against the materialised project-default issuer passes the serve
// gate on a private remote server with no explicit issuer and proxies
// through to the upstream — the runtime half of the implicit OAuth loop.
func TestServeMCP_ImplicitIssuer_JWTHappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	slug, mcpServer, _ := seedRemoteMCPEndpoint(t, ctx, ti, *authCtx.ProjectID, upstream.URL, "private")
	require.False(t, mcpServer.UserSessionIssuerID.Valid, "fixture must have no explicit issuer")

	// Materialise the default issuer the way an OAuth entry point would,
	// then mint a JWT bound to it — the same audience the serve gate
	// resolves read-only.
	issuer, err := usersessions.GetOrCreateDefaultIssuer(ctx, ti.conn, *authCtx.ProjectID)
	require.NoError(t, err)

	mcpEndpoint, err := mcpendpointsrepo.New(ti.conn).GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpointsrepo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           slug,
		CustomDomainID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	endpoint := mcp.NewResolvedMcpEndpointFromMcpServer(&mcpEndpoint, &mcpServer, project.OrganizationID, issuer.ID)

	// Private endpoints route through the IDP, which stamps a user subject.
	subject := urn.NewUserSubject("user_" + uuid.NewString()[:8])
	accessToken := mintIssuerGatedAccessToken(t, ctx, ti, slug, endpoint, issuer.ID, subject)

	rr := runHandler(t, ctx, ti, http.MethodPost, slug, bearer(accessToken), []byte(initializeBody))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("upstream not invoked within 5s; status=%d body=%s", rr.Code, rr.Body.String())
	}
	require.Equal(t, http.StatusOK, rr.Code, "implicitly gated bearer must pass the serve gate; body=%s", rr.Body.String())
}
