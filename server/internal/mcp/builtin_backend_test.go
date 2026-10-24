package mcp_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// builtinCalls records the identity each request reached the backend with.
type builtinCalls struct {
	mu         sync.Mutex
	identities []mcpidentity.Identity
}

func (c *builtinCalls) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, _ := mcpidentity.FromContext(r.Context())
	c.mu.Lock()
	c.identities = append(c.identities, identity)
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
}

func (c *builtinCalls) seen() []mcpidentity.Identity {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]mcpidentity.Identity(nil), c.identities...)
}

// createBuiltinServer creates a project server served by a first-party
// backend, signed in to through issuerID, with an endpoint, and registers a
// backend with one tool that records its callers.
func createBuiltinServer(t *testing.T, ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID) (string, mcpserversrepo.McpServer, *builtinCalls) {
	t.Helper()

	backend := "test-builtin-" + uuid.NewString()[:8]
	calls := &builtinCalls{mu: sync.Mutex{}, identities: nil}
	ti.service.SetBuiltinBackend(backend, mcp.BuiltinBackend{
		Handler: calls,
		Tools:   []*mcpsdk.Tool{{Name: "builtin_tool", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}}},
	})

	created, err := approvalrepo.New(ti.conn).CreateBuiltinMCPServer(ctx, approvalrepo.CreateBuiltinMCPServerParams{
		ID:                uuid.New(),
		ProjectID:         projectID,
		Name:              conv.ToPGText("Builtin"),
		Slug:              conv.ToPGText(backend),
		BuiltinBackend:    conv.ToPGText(backend),
		Visibility:        visibility.Private,
		NetworkAccessMode: networkaccess.Storage(networkaccess.ModePublicOnly),
	})
	require.NoError(t, err)
	require.NoError(t, approvalrepo.New(ti.conn).SetBuiltinMCPServerUserSessionIssuer(ctx, approvalrepo.SetBuiltinMCPServerUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		ID:                  created.ID,
		ProjectID:           projectID,
	}))
	endpointSlug := "endpoint-" + uuid.NewString()
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       projectID,
		CustomDomainID:  uuid.NullUUID{},
		McpServerID:     uuid.NullUUID{UUID: created.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{},
		Slug:            endpointSlug,
	})
	require.NoError(t, err)

	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: created.ID, ProjectID: projectID})
	require.NoError(t, err)
	return endpointSlug, server, calls
}

func TestServePublic_BuiltinBackend_ChallengesCallersWithoutACredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	endpointSlug, _, calls := createBuiltinServer(t, ctx, ti, *authCtx.ProjectID, createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID))

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), "", nil)
	require.Error(t, err, "a request without a credential is rejected")
	expected := `Bearer resource_metadata="` + ti.serverURL.String() + `/.well-known/oauth-protected-resource/mcp/` + endpointSlug + `"`
	require.Equal(t, expected, w.Header().Get("WWW-Authenticate"), "the challenge starts the client's sign-in")
	require.Empty(t, calls.seen(), "an unauthenticated request must not reach the backend")
}

func TestServePublic_BuiltinBackend_SignsInUsersThroughItsIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	endpointSlug, server, calls := createBuiltinServer(t, ctx, ti, *authCtx.ProjectID, createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID))
	token := mintIssuerBearerForEndpointSubject(t, ctx, ti, endpointSlug, server, authCtx.ActiveOrganizationID, urn.NewUserSubject(authCtx.UserID))

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	seen := calls.seen()
	require.Len(t, seen, 1)
	require.Equal(t, mcpidentity.KindUserSession, seen[0].Kind())
	require.Equal(t, authCtx.UserID, seen[0].UserID())
}

func TestServePublic_BuiltinBackend_AcceptsKeysWithoutTheIssuer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	endpointSlug, _, calls := createBuiltinServer(t, ctx, ti, *authCtx.ProjectID, createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID))

	// The issuer gate does not accept a plain API key, so a key that reaches
	// the backend shows keys authenticate on their own path.
	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), ti.createTestAPIKey(ctx, t), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	seen := calls.seen()
	require.Len(t, seen, 1)
	require.Equal(t, mcpidentity.KindAPIKey, seen[0].Kind())
}

// A key reaches the backend only in the server's project: an organization-wide
// key is admitted, a key bound to another project is not.
func TestServePublic_BuiltinBackend_AdmitsKeysOnlyInItsProject(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		otherProject bool
	}{
		{name: "organization", otherProject: false},
		{name: "other project", otherProject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			endpointSlug, _, calls := createBuiltinServer(t, ctx, ti, *authCtx.ProjectID, createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID))

			keyOwner := *authCtx
			keyOwner.ProjectID = nil
			keyOwner.ProjectSlug = nil
			if tc.otherProject {
				other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
					Name: "Other project", Slug: "other-project", OrganizationID: authCtx.ActiveOrganizationID,
				})
				require.NoError(t, err)
				keyOwner.ProjectID = &other.ID
			}
			key := ti.createTestAPIKey(contextvalues.SetAuthContext(ctx, &keyOwner), t)

			w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), key, nil)
			if tc.otherProject {
				require.ErrorContains(t, err, "key project does not match the mcp server project")
				require.Empty(t, calls.seen(), "a key from another project must not reach the backend")
				return
			}
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
			require.Len(t, calls.seen(), 1)
		})
	}
}

// The consent screen lists a first-party backend's tools before the user signs
// in, without reaching the backend.
func TestServeConsentMCP_BuiltinBackendListsItsTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	endpointSlug, server, calls := createBuiltinServer(t, ctx, ti, issuer.ProjectID.UUID, issuer.ID)
	stateID, csrfToken := seedModernConsentChallenge(t, ctx, ti, issuer.ID, client, server.ID, endpointSlug)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, endpointSlug, "x/mcp")
	require.NoError(t, err)

	w := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrfToken, uuid.NewString(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, nil)
	require.Contains(t, w.Body.String(), `"name":"builtin_tool"`)
	require.Contains(t, w.Body.String(), `"readOnlyHint":true`)
	require.Empty(t, calls.seen(), "listing tools for consent must not call the backend")
}
