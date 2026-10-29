package remotesessions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	wire "github.com/speakeasy-api/gram/server/gen/http/remote_sessions/server"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestServerConnectionUsesSignedInUserAndDoesNotExecuteTools(t *testing.T) {
	t.Parallel()
	var methods sync.Map
	server := mcp.NewServer(&mcp.Implementation{Name: "setup-upstream", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "example", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t.Error("executed a tool")
		return nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-access-sentinel" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read MCP request: %v", err)
			http.Error(w, "read request", http.StatusBadRequest)
			return
		}
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if message.Method != "" {
			methods.Store(message.Method, true)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	ctx, env := newSyntheticExpiryEnv(t, "setup-check", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"user-access-sentinel","token_type":"Bearer","expires_in":3600,"scope":"channels:history"}`))
	}, withRemoteServer(upstream.URL))
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	env.service.SetServerConnectionTester(platformmcp.NewRemoteMCPReadinessProber(testenv.NewLogger(t), env.db, env.enc, policy, env.mgr))
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	signedInUser := *authCtx
	signedInUser.UserID = env.subject.ID
	signedIn := contextvalues.WithValidatedGramSession(ctx, &signedInUser, false)
	payload := &gen.TestServerConnectionPayload{McpServerID: env.mcpServerID.UUID.String()}
	result, err := env.service.TestServerConnection(signedIn, payload)
	require.NoError(t, err)
	require.Equal(t, "ready", result.Status)
	require.Equal(t, 1, *result.ToolCount)
	encoded, err := json.Marshal(wire.NewTestServerConnectionResponseBody(result))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "user-access-sentinel")
	_, initialized := methods.Load("initialize")
	require.True(t, initialized)
	_, listed := methods.Load("tools/list")
	require.True(t, listed)
	_, called := methods.Load("tools/call")
	require.False(t, called)
	other := *authCtx
	other.UserID = "another-user"
	result, err = env.service.TestServerConnection(contextvalues.WithValidatedGramSession(ctx, &other, false), payload)
	require.NoError(t, err)
	require.Equal(t, "authorization_required", result.Status)
	require.Nil(t, result.ToolCount)
	denied := withExactAccessGrants(t, signedIn, env.db)
	result, err = env.service.TestServerConnection(denied, payload)
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)
	keyCtx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{UserID: authCtx.UserID, ProjectID: authCtx.ProjectID, ActiveOrganizationID: authCtx.ActiveOrganizationID, APIKeyID: uuid.NewString()})
	_, err = env.service.TestServerConnection(keyCtx, payload)
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestServerConnectionDoesNotExposeForeignProjectServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = contextvalues.WithValidatedGramSession(ctx, authCtx, false)
	// A valid connect grant must not bypass project-qualified selection.
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource))
	otherProject := createProject(t, ctx, ti.conn, "setup-foreign-project")
	issuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "setup-foreign-issuer")
	foreign := createImpactServer(t, ctx, ti, otherProject, "setup-foreign-server", issuer)
	result, err := ti.service.TestServerConnection(ctx, &gen.TestServerConnectionPayload{McpServerID: foreign.String()})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetupSaveRefusesSharedIdentityEvenWithWriteAccess(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	target, issuer := createServerIdentityTarget(t, ctx, ti, "setup-target")
	sibling := createServerIdentityTargetOnIssuer(t, ctx, ti, "setup-sibling", issuer)
	provider := createServerIdentityProvider(t, ctx, ti, "setup-provider", "", false, []string{"client_secret_basic"})
	payload := manualExistingProviderIdentityPayload(target, provider, "setup-client")
	payload.TargetServerOnly = new(true)
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Contains(t, err.Error(), "dedicated User Identity issuer")
	clients, err := repo.New(ti.conn).ListRemoteSessionClientsForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerParams{ProjectID: conv.ToNullUUID(projectIDFromContext(t, ctx)), OrganizationID: conv.ToPGText(activeOrganizationID(t, ctx)), UserSessionIssuerID: issuer})
	require.NoError(t, err)
	require.Empty(t, clients)
	require.NotEqual(t, target, sibling)
}

func TestSetupSaveRefusesSharedIdentityAcrossProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "setup-shared-org-issuer")
	target := createServerIdentityTargetOnIssuer(t, ctx, ti, "setup-shared-org-target", issuer)
	otherProject := createProject(t, ctx, ti.conn, "setup-shared-other-project")
	_ = createImpactServer(t, ctx, ti, otherProject, "setup-shared-other-server", issuer)
	provider := createServerIdentityProvider(t, ctx, ti, "setup-shared-org-provider", "", false, []string{"client_secret_basic"})
	payload := manualExistingProviderIdentityPayload(target, provider, "setup-org-client")
	payload.TargetServerOnly = new(true)
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestServerConnectionRefusesStaticAuthorizationHeader(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "static credential must not be used", http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)
	ctx, env := newSyntheticExpiryEnv(t, "setup-static-header", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"saved-user-token","token_type":"Bearer","expires_in":3600,"scope":"channels:history"}`))
	}, withRemoteServer(upstream.URL))
	target, err := mcpserversrepo.New(env.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: env.mcpServerID.UUID, ProjectID: env.projectID})
	require.NoError(t, err)
	encrypted, err := env.enc.Encrypt([]byte("Bearer static-credential-sentinel"))
	require.NoError(t, err)
	_, err = remotemcprepo.New(env.db).CreateServerHeader(ctx, remotemcprepo.CreateServerHeaderParams{ProjectID: env.projectID, RemoteMcpServerID: target.RemoteMcpServerID.UUID, Name: "aUtHoRiZaTiOn", IsSecret: true, Value: conv.ToPGText(encrypted)})
	require.NoError(t, err)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	env.service.SetServerConnectionTester(platformmcp.NewRemoteMCPReadinessProber(testenv.NewLogger(t), env.db, env.enc, policy, env.mgr))
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	signedInUser := *authCtx
	signedInUser.UserID = env.subject.ID
	result, err := env.service.TestServerConnection(contextvalues.WithValidatedGramSession(ctx, &signedInUser, false), &gen.TestServerConnectionPayload{McpServerID: env.mcpServerID.UUID.String()})
	require.NoError(t, err)
	require.Equal(t, "configuration_required", result.Status)
	require.Equal(t, "authorization_header_conflict", result.Code)
	require.Nil(t, result.ToolCount)
	require.Zero(t, requests.Load())
}
