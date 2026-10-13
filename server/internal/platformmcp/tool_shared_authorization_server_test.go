package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

// recordingSharedIssuerProvisioner captures the auth context and payload the
// tool hands the management API and answers with a fixed issuer or error.
type recordingSharedIssuerProvisioner struct {
	issuer  *types.UserSessionIssuer
	err     error
	authCtx *contextvalues.AuthContext
	payload *orggen.CreateSharedIssuerPayload
}

func (p *recordingSharedIssuerProvisioner) CreateSharedIssuer(ctx context.Context, payload *orggen.CreateSharedIssuerPayload) (*types.UserSessionIssuer, error) {
	p.authCtx, _ = contextvalues.GetAuthContext(ctx)
	p.payload = payload
	return p.issuer, p.err
}

func newSharedAuthorizationServerSession(t *testing.T, provisioner SharedAuthorizationServerProvisioner, principal Principal, grants []authz.Grant) *mcp.ClientSession {
	t.Helper()
	server := newTestMCPServer()
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			ctx = contextWithPrincipal(ctx, principal)
			ctx = authz.GrantsToContext(ctx, grants)
			ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID})
			return next(ctx, method, req)
		}
	})
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerCreateSharedAuthorizationServerTool(reg, provisioner)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "shared-authorization-server-test", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestCreateSharedAuthorizationServerUnavailableThroughMCP(t *testing.T) {
	t.Parallel()
	server, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			ctx = contextWithPrincipal(ctx, testPrincipal())
			ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, testPrincipal().OrganizationID)})
			return next(ctx, method, req)
		}
	})
	registrar.withExternalAuthorizer(allowExternalCallAuthorizer{})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "shared-authorization-server-test", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	var found *mcp.Tool
	for tool, err := range session.Tools(t.Context(), nil) {
		require.NoError(t, err)
		if tool.Name == "create_shared_authorization_server" {
			found = tool
		}
	}
	require.NotNil(t, found)
	require.NotNil(t, found.Annotations)
	require.True(t, found.Annotations.IdempotentHint)
	require.False(t, found.Annotations.ReadOnlyHint)
	require.Nil(t, found.Annotations.DestructiveHint)

	refusal := callSkillsRefusal(t, t.Context(), session, "create_shared_authorization_server", map[string]any{"idempotency_key": uuid.NewString(), "confirmed": true})
	require.Equal(t, "feature_unavailable", refusal.Code)
}

func TestCreateSharedAuthorizationServerToolContract(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	principal := testPrincipal()
	admin := []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)}

	issuerID := uuid.NewString()
	issuerURL := "https://app.example.com" + authserver.SharedPath(uuid.MustParse(issuerID))
	created := &types.UserSessionIssuer{ID: issuerID, OrganizationID: principal.OrganizationID, Slug: "shared-authorization-server-" + issuerID[:8], AuthorizationServerMode: string(authserver.ModeShared), IssuerURL: &issuerURL}
	provisioner := &recordingSharedIssuerProvisioner{issuer: created}
	session := newSharedAuthorizationServerSession(t, provisioner, principal, admin)

	require.Equal(t, "confirmation_required", callSkillsRefusal(t, ctx, session, "create_shared_authorization_server", map[string]any{"idempotency_key": "setup-1", "confirmed": false}).Code)
	require.Equal(t, "invalid_request", callSkillsRefusal(t, ctx, session, "create_shared_authorization_server", map[string]any{"idempotency_key": "  ", "confirmed": true}).Code)
	require.Nil(t, provisioner.payload, "refusals never reach the management API")

	output := callSkillsTool[CreateSharedAuthorizationServerOutput](t, ctx, session, "create_shared_authorization_server", map[string]any{"idempotency_key": "setup-1", "confirmed": true})
	require.Equal(t, issuerID, output.UserSessionIssuerID)
	require.Equal(t, created.Slug, output.Slug)
	require.Equal(t, string(authserver.ModeShared), output.AuthorizationServerMode)
	require.Equal(t, issuerURL, output.Issuer)
	require.Equal(t, issuerURL+"/token", output.TokenEndpoint)
	require.NotEmpty(t, output.Message)
	require.Equal(t, "setup-1", provisioner.payload.IdempotencyKey)
	require.NotNil(t, provisioner.authCtx)
	require.Equal(t, principal.OrganizationID, provisioner.authCtx.ActiveOrganizationID)
	require.Equal(t, principal.UserID, provisioner.authCtx.UserID)
	require.Nil(t, provisioner.authCtx.ProjectID)
	require.Empty(t, provisioner.authCtx.APIKeyID)

	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"forbidden", oops.E(oops.CodeForbidden, nil, "forbidden"), "permission_denied"},
		{"unauthorized", oops.C(oops.CodeUnauthorized), "permission_denied"},
		{"bad request", oops.E(oops.CodeBadRequest, nil, "bad key"), "invalid_request"},
		{"conflict", oops.E(oops.CodeConflict, nil, "key reused"), "conflict"},
		{"unexpected", oops.E(oops.CodeUnexpected, nil, "unserved"), "feature_unavailable"},
	} {
		refusing := newSharedAuthorizationServerSession(t, &recordingSharedIssuerProvisioner{err: tc.err}, principal, admin)
		refusal := callSkillsRefusal(t, ctx, refusing, "create_shared_authorization_server", map[string]any{"idempotency_key": "setup-1", "confirmed": true})
		require.Equal(t, tc.code, refusal.Code, tc.name)
		require.NotContains(t, refusal.Message, tc.err.Error(), "internal error text stays out of the refusal")
	}
}

// The tool runs the management API's own creation: an organization
// administrator gets a pinned shared issuer, a replay gets the same one, and
// a member without org:admin is refused by the same authorization check.
func TestCreateSharedAuthorizationServerThroughMCP(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_shared_authorization_server")
	require.NoError(t, err)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("platform-mcp-shared-"+uuid.NewString()), billing.NewStubClient(logger, tracerProvider))
	engine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	const serverURL = "https://app.example.com"
	service := usersessions.NewService(logger, tracerProvider, testenv.NewMeterProvider(t), conn, sessionManager, nil, engine, audit.NewLogger(), policy, nil, testenv.NewEncryptionClient(t), usersessions.NewSigner("test-jwt-secret"), serverURL, authserver.Hosts{ServerURL: serverURL, AuthenticationHostBaseURL: "", PlatformHosts: nil}, ratelimit.NewRedisStore(redisClient))

	args := map[string]any{"idempotency_key": "platform-setup-" + uuid.NewString(), "confirmed": true}
	member := newSharedAuthorizationServerSession(t, service, principal, []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, principal.OrganizationID)})
	require.Equal(t, "permission_denied", callSkillsRefusal(t, ctx, member, "create_shared_authorization_server", args).Code)

	admin := newSharedAuthorizationServerSession(t, service, principal, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)})
	first := callSkillsTool[CreateSharedAuthorizationServerOutput](t, ctx, admin, "create_shared_authorization_server", args)
	require.Equal(t, string(authserver.ModeShared), first.AuthorizationServerMode)
	require.Equal(t, serverURL+authserver.SharedPath(uuid.MustParse(first.UserSessionIssuerID)), first.Issuer)
	require.Equal(t, first.Issuer+"/token", first.TokenEndpoint)

	again := callSkillsTool[CreateSharedAuthorizationServerOutput](t, ctx, admin, "create_shared_authorization_server", args)
	require.Equal(t, first, again)

	adminCtx := contextvalues.SetAuthContext(authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)}), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID})
	listed, err := service.ListIssuers(adminCtx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, first.UserSessionIssuerID, listed.Items[0].ID)
}
