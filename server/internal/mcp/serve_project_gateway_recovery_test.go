package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func extractRecoveryPath(t *testing.T, siteURL *url.URL, text string) string {
	t.Helper()
	prefix := siteURL.JoinPath("mcp", "connection_recovery").String() + "/"
	idx := strings.Index(text, prefix)
	require.NotEqual(t, -1, idx, "expected recovery link starting with %s in %q", prefix, text)
	raw := strings.Fields(text[idx:])[0]
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed.Path
}

func seedProfileChoice(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	organizationID string,
	serverID uuid.UUID,
	authClientID uuid.UUID,
) uuid.UUID {
	t.Helper()
	versionID := uuid.New()
	rows, err := upstreamclientsrepo.New(ti.conn).RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
		CredentialKind: upstreamclients.CredentialKindExecutionProfileVersion,
		CredentialID:   versionID,
		Source:         "assistant_profile",
		OrganizationID: organizationID,
		AuthClientID:   uuid.NullUUID{UUID: authClientID, Valid: true},
		McpServerID:    serverID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	return versionID
}

func TestServeProjectGateway_PersonalConnectionRecovery(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)
	ti.service.SetAuthClientPermitted(true)

	// Separate the represented user from the agent owner so tests prove the
	// recovery link binds strictly to AuthorizerUserID, never the owner.
	humanUser := "user_human_" + uuid.NewString()[:8]
	q := testrepo.New(ti.conn)
	require.NoError(t, q.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID: humanUser, Email: humanUser + "@example.com", DisplayName: "Human Requester",
	}))
	require.NoError(t, q.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: authCtx.ActiveOrganizationID, UserID: pgtype.Text{String: humanUser, Valid: true},
	}))

	var upstreamStatus atomic.Int32
	upstreamStatus.Store(http.StatusUnauthorized)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		status := int(upstreamStatus.Load())
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(upstream.Close)

	asBase := "https://recovery-gw-" + uuid.NewString()[:6] + ".example.com"
	registration := createConsentRemoteClientWithCallback(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "gw-rec-"+uuid.NewString()[:6], asBase, "")

	fixtures := authclientfixtures.New(ti.conn)
	remoteID, serverID := uuid.New(), uuid.New()
	memberSlug := "linear-" + serverID.String()[:6]
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, authclientfixtures.SeedRemoteMCPServerFixtureParams{
		ID: remoteID, ProjectID: *authCtx.ProjectID, Url: upstream.URL,
	}))
	require.NoError(t, fixtures.SeedMCPServerFixture(ctx, authclientfixtures.SeedMCPServerFixtureParams{
		ID:                serverID,
		ProjectID:         *authCtx.ProjectID,
		Slug:              pgtype.Text{String: memberSlug, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true},
	}))
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		ClientType:            "remote_session",
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		McpServerID: serverID, AuthClientID: authClientID, Source: "admin",
	}))
	associateServerRegistration(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, serverID, registration)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Gateway recovery agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	grantProjectRead(t, ti, authCtx)
	projectReadSel, err := authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, humanUser),
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      projectReadSel,
	})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, serverID.String())
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, humanUser, serverID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), serverID)

	profileVersionID := seedProfileChoice(t, ctx, ti, authCtx.ActiveOrganizationID, serverID, authClientID)
	binding := seedAssistantThreadBinding(t, ctx, ti, authCtx)
	token, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		AuthorizerUserID:          humanUser,
		SelectionProfileVersionID: uuid.NullUUID{UUID: profileVersionID, Valid: true},
	}, binding)
	require.NoError(t, err)

	// Configure browser session auth for verifying recovery link binding.
	ti.service.SetBrowserSession(func(ctx context.Context, cookie string) (context.Context, error) {
		if cookie == "" {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: "", UserID: cookie}), nil
	})
	router := chi.NewRouter()
	router.Get(mcp.ConnectionRecoveryRoute, oops.BrowserErrHandle(ti.logger, ti.service.HandleConnectionRecovery).ServeHTTP)
	openRecovery := func(path, userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		if userID != "" {
			req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: userID})
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Unconnected personal client on execute_tool yields "Connection required"
	// with a one-time link bound strictly to humanUser.
	executed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": memberSlug + "--list_issues", "arguments": map[string]any{}},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.True(t, isError)
	require.Contains(t, text, "Connection required: the person this turn acts for needs to connect their own account")
	require.NotContains(t, text, "mcp-sign-in")

	recoveryPath := extractRecoveryPath(t, ti.siteURL, text)
	ownerRes := openRecovery(recoveryPath, authCtx.UserID)
	require.Equal(t, http.StatusForbidden, ownerRes.Code, "agent owner must not be able to use another user's recovery link")
	require.Contains(t, ownerRes.Body.String(), "for someone else")

	humanRes := openRecovery(recoveryPath, humanUser)
	require.Equal(t, http.StatusFound, humanRes.Code, humanRes.Body.String())
	location, err := url.Parse(humanRes.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, asBase+"/authorize", location.Scheme+"://"+location.Host+location.Path)
	remote, err := ti.service.RemoteLoginState(ctx, location.Query().Get("state"))
	require.NoError(t, err)
	require.NotNil(t, remote.Subject)
	require.Equal(t, humanUser, remote.Subject.ID)

	secondOpen := openRecovery(recoveryPath, humanUser)
	require.Equal(t, http.StatusNotFound, secondOpen.Code, "recovery link is single-use")

	// 2. describe_server on the unconnected member also yields "Connection required".
	described, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeServer,
		"arguments": map[string]any{"server": memberSlug},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, described.Code, described.Body.String())
	descText, descErr := metaToolResultText(t, decodeRPCResponse(t, described))
	require.True(t, descErr)
	require.Contains(t, descText, "Connection required:")

	// 3. connection_status is a read-only probe and does not mint recovery links.
	statusResp, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolConnectionStatus,
		"arguments": map[string]any{},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, statusResp.Code, statusResp.Body.String())
	require.NotContains(t, statusResp.Body.String(), "/mcp/connection_recovery/")

	// 4. Expired stored session (no refresh token) yields "Reconnect required".
	expiredEnc, err := ti.enc.Encrypt([]byte("expired-personal-token"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSessionFixture(ctx, remotesessions_repo.UpsertRemoteSessionFixtureParams{
		SubjectUrn:            urn.NewUserSubject(humanUser),
		UserSessionIssuerID:   fixture.issuerID,
		RemoteSessionClientID: registration,
		AccessTokenEncrypted:  expiredEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		RefreshTokenEncrypted: pgtype.Text{String: "", Valid: false},
		RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		Scopes:                []string{},
		Resource:              conv.ToPGText(upstream.URL),
	})
	require.NoError(t, err)

	expiredExec, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": memberSlug + "--list_issues", "arguments": map[string]any{}},
	}))
	require.NoError(t, err)
	expiredText, expiredErr := metaToolResultText(t, decodeRPCResponse(t, expiredExec))
	require.True(t, expiredErr)
	require.Contains(t, expiredText, "Reconnect required: the person this turn acts for needs to reconnect their own account")
	_ = extractRecoveryPath(t, ti.siteURL, expiredText)

	// 5. Valid stored session rejected with upstream 401 yields "Reconnect required".
	liveEnc, err := ti.enc.Encrypt([]byte("revoked-upstream-token"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSessionFixture(ctx, remotesessions_repo.UpsertRemoteSessionFixtureParams{
		SubjectUrn:            urn.NewUserSubject(humanUser),
		UserSessionIssuerID:   fixture.issuerID,
		RemoteSessionClientID: registration,
		AccessTokenEncrypted:  liveEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		RefreshTokenEncrypted: pgtype.Text{String: "", Valid: false},
		RefreshExpiresAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		Scopes:                []string{},
		Resource:              conv.ToPGText(upstream.URL),
	})
	require.NoError(t, err)

	rejectedExec, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": memberSlug + "--list_issues", "arguments": map[string]any{}},
	}))
	require.NoError(t, err)
	rejectedText, rejectedErr := metaToolResultText(t, decodeRPCResponse(t, rejectedExec))
	require.True(t, rejectedErr)
	require.Contains(t, rejectedText, "Reconnect required: the person this turn acts for needs to reconnect their own account")
	_ = extractRecoveryPath(t, ti.siteURL, rejectedText)

	// 6. Upstream 403 (account lacks upstream permission, not credential rejection)
	// does NOT offer a personal reconnect link.
	upstreamStatus.Store(http.StatusForbidden)
	forbiddenExec, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": memberSlug + "--list_issues", "arguments": map[string]any{}},
	}))
	require.NoError(t, err)
	forbiddenText, forbiddenErr := metaToolResultText(t, decodeRPCResponse(t, forbiddenExec))
	require.True(t, forbiddenErr)
	require.NotContains(t, forbiddenText, "/mcp/connection_recovery/")
	require.NotContains(t, forbiddenText, "Reconnect required")

	// 7. Telemetry logs record the failed calls and never persist one-time recovery URLs.
	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'list_issues'
		   AND position(toJSONString(attributes), 'personal connection') > 0`,
		3, authCtx.ProjectID.String())
	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND position(toJSONString(attributes), '/mcp/connection_recovery/') > 0`,
		0, authCtx.ProjectID.String())
}

func TestServeProjectGateway_ServiceAccountClientSkipsPersonalRecovery(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)
	ti.service.SetAuthClientPermitted(true)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	asBase := "https://sa-" + uuid.NewString()[:6] + ".example.com"
	saClientID := createConsentRemoteClientWithCallback(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "gw-sa-"+uuid.NewString()[:6], asBase, "")
	testFixtures := testrepo.New(ti.conn)
	rows, err := testFixtures.ForceRemoteSessionClientAuthMethodFixture(ctx, testrepo.ForceRemoteSessionClientAuthMethodFixtureParams{
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"),
		ID:                      saClientID,
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	rows, err = testFixtures.ForceRemoteSessionClientCredentialOwnerFixture(ctx, testrepo.ForceRemoteSessionClientCredentialOwnerFixtureParams{
		CredentialOwner: string(remotesessions.CredentialOwnerSelf),
		ID:              saClientID,
		ProjectID:       conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:  conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	fixtures := authclientfixtures.New(ti.conn)
	remoteID, serverID := uuid.New(), uuid.New()
	memberSlug := "sa-srv-" + serverID.String()[:6]
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, authclientfixtures.SeedRemoteMCPServerFixtureParams{
		ID: remoteID, ProjectID: *authCtx.ProjectID, Url: upstream.URL,
	}))
	require.NoError(t, fixtures.SeedMCPServerFixture(ctx, authclientfixtures.SeedMCPServerFixtureParams{
		ID:                serverID,
		ProjectID:         *authCtx.ProjectID,
		Slug:              pgtype.Text{String: memberSlug, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true},
	}))
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		ClientType:            "service_account",
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: saClientID, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		McpServerID: serverID, AuthClientID: authClientID, Source: "admin",
	}))
	associateServerRegistration(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, serverID, saClientID)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "SA recovery agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	grantProjectRead(t, ti, authCtx)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, serverID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), serverID)

	profileVersionID := seedProfileChoice(t, ctx, ti, authCtx.ActiveOrganizationID, serverID, authClientID)
	binding := seedAssistantThreadBinding(t, ctx, ti, authCtx)
	token, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		AuthorizerUserID:          authCtx.UserID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: profileVersionID, Valid: true},
	}, binding)
	require.NoError(t, err)

	executed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": memberSlug + "--list_issues", "arguments": map[string]any{}},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.True(t, isError)
	require.NotContains(t, text, "/mcp/connection_recovery/")
	require.NotContains(t, text, "Connection required:")
	require.NotContains(t, text, "Reconnect required:")
}

func TestServeProjectGateway_WorkloadTurnKeepsAgentFix(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, f.ctx, f.ti)
	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)
	f.ti.service.SetAuthClientPermitted(true)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	asBase := "https://workload-gw-" + uuid.NewString()[:6] + ".example.com"
	registration := createConsentRemoteClientWithCallback(t, f.ctx, f.ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "gw-wl-"+uuid.NewString()[:6], asBase, "")

	fixtures := authclientfixtures.New(f.ti.conn)
	remoteID, serverID := uuid.New(), uuid.New()
	memberSlug := "wl-linear-" + serverID.String()[:6]
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(f.ctx, authclientfixtures.SeedRemoteMCPServerFixtureParams{
		ID: remoteID, ProjectID: *authCtx.ProjectID, Url: upstream.URL,
	}))
	require.NoError(t, fixtures.SeedMCPServerFixture(f.ctx, authclientfixtures.SeedMCPServerFixtureParams{
		ID:                serverID,
		ProjectID:         *authCtx.ProjectID,
		Slug:              pgtype.Text{String: memberSlug, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true},
	}))
	authClientID, err := fixtures.SeedAuthClientFixture(f.ctx, authclientfixtures.SeedAuthClientFixtureParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		ClientType:            "remote_session",
		Source:                "admin",
		RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(f.ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		McpServerID: serverID, AuthClientID: authClientID, Source: "admin",
	}))
	associateServerRegistration(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, serverID, registration)

	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, serverID.String())
	seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), serverID)

	profileVersionID := seedProfileChoice(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, serverID, authClientID)
	binding := seedAssistantThreadBinding(t, f.ctx, f.ti, authCtx)
	token, err := f.ti.assistantTokens.MintTurnCredential(f.ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewWorkloadPrincipal(f.issuerID, f.subject),
		AuthorizerUserID:          "",
		SelectionProfileVersionID: uuid.NullUUID{UUID: profileVersionID, Valid: true},
	}, binding)
	require.NoError(t, err)

	executed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": memberSlug + "--list_issues", "arguments": map[string]any{}},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.True(t, isError)
	require.NotContains(t, text, "/mcp/connection_recovery/")
	require.NotContains(t, text, "Connection required:")
	require.Contains(t, text, "mcp-sign-in")
}

func TestServeProjectGateway_TurnCredentialChatIDFallback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)
	grantProjectRead(t, ti, authCtx)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Chat fallback agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	binding := seedAssistantThreadBinding(t, ctx, ti, authCtx)
	token, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		AuthorizerUserID:          authCtx.UserID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, binding)
	require.NoError(t, err)

	// Request carries no Gram-Chat-ID header; admitMetaGate must fall back to
	// the bound thread's chat ID on the turn credential.
	listed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())

	gatewayID := orggateway.ProjectGatewayID(authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND meta_mcp_server_id = ?
		   AND event_source = 'meta_discovery'
		   AND tool_name = 'list_servers'
		   AND toString(attributes.gen_ai.conversation.id) = ?`,
		1, authCtx.ProjectID.String(), gatewayID.String(), binding.ChatID.String())
}
