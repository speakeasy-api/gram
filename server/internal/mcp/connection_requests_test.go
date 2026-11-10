package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/sessionclientinfo"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	metadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The healthy path: an authenticated tools/call forwarded to a working server writes nothing.
func TestHealthyToolCallRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	var capturedAuth atomic.Value
	upstream := newAuthorizationRecorder(t, &capturedAuth)

	fixture := createIssuerGatedHTTPToolFixture(t, ctx, ti, authCtx, "healthy-call", upstream.URL)
	subject := urn.NewUserSubject("healthy-user-" + uuid.NewString())
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, "valid-upstream-token", time.Now().Add(time.Hour))
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	resp, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code)

	call, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": fixture.ToolName, "arguments": map[string]any{}}})
	require.NoError(t, err)
	resp, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, call, bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	rec.RequireNothing()
}

// A client's first handshake carries no credentials. It is how sign-in starts.
func TestNoCredentialsRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-no-credentials")

	_, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.Error(t, err)
	rec.RequireNothing()
}

// Expiry starts routine silent refresh, not an actionable reconnection failure.
func TestExpiredBearerRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-expired")

	token, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject("expired-user"),
		Audience: urn.NewToolset(fixture.Toolset.ID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + fixture.Toolset.McpSlug.String,
		Lifetime: -time.Minute,
	})
	require.NoError(t, err)

	_, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), token, nil)
	require.Error(t, err)
	rec.RequireNothing()
}

// A bearer of ours names someone. One that is not ours would let a scanner write rows.
func TestRejectedBearer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-rejected")

	_, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), "not-a-token", nil)
	require.Error(t, err)
	rec.RequireNothing()

	// Ours, but minted for another server: record the wrong-audience refusal.
	subject := urn.NewUserSubject("rejected-user")
	elsewhere, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: "urn:some-other-server",
		Issuer:   ti.serverURL.String() + "/mcp/" + fixture.Toolset.McpSlug.String,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	_, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), elsewhere, nil)
	require.Error(t, err)
	wrongAudience := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)
	require.Equal(t, subject.String(), wrongAudience.SubjectURN)
	rec = mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(fixture.Toolset.ID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + fixture.Toolset.McpSlug.String,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, fixture.UserSessionIssuer.ID, subject, jti)
	require.NoError(t, ti.chatSessionsManager.RevokeToken(ctx, jti))
	_, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), token, nil)
	require.Error(t, err)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
	require.Equal(t, subject.String(), row.SubjectURN)
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
}

// A client that aborts during a refresh says nothing about the provider.
func TestAbortedRefreshRecordsNoRejection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-aborted")

	subject := urn.NewUserSubject("gate-aborted-user-" + uuid.NewString())
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject)
	requestCtx, abort := context.WithCancel(context.Background())
	defer abort()
	released := make(chan struct{})
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, func(http.ResponseWriter, *http.Request) {
		abort()
		<-released
	})
	t.Cleanup(func() { close(released) })
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	_, err := servePublicHTTP(t, requestCtx, ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Error(t, err)
	require.Error(t, requestCtx.Err(), "the token endpoint must have been reached")
	rec.RequireNothing()
}

func TestRemoteSessionRejections(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		upstream http.HandlerFunc
		want     mcpconnectionattempts.Code
		who      mcpconnectionattempts.WhoFixes
		detail   string
	}{
		{name: "missing", want: mcpconnectionattempts.CodeInvalidRemoteSession, who: mcpconnectionattempts.WhoPerson, detail: "invalid_remote_session"},
		{name: "outage", upstream: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }, want: mcpconnectionattempts.CodeRemoteSessionUnavailable, who: mcpconnectionattempts.WhoProvider, detail: "remote_session_unavailable"},
		{name: "misconfigured", upstream: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
		}, want: mcpconnectionattempts.CodeCredentialMisconfigured, who: mcpconnectionattempts.WhoAdmin, detail: "remote_session_misconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-"+tc.name)

			subject := urn.NewUserSubject("gate-user-" + uuid.NewString())
			if tc.upstream != nil {
				upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject)
				pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, tc.upstream)
			}
			bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

			_, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, map[string]string{"User-Agent": "claude-code/2.1.0"})
			require.Error(t, err)

			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, mcpconnectionattempts.FamilyClaudeCode, row.ClientFamily)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Equal(t, tc.detail, row.SubReason)
			require.Equal(t, subject.String(), row.SubjectURN)
		})
	}
}

// A proxied server's failure lands on its own row, through the real request path.
func TestProxiedUpstreamFailureRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "endpoint-" + uuid.NewString()
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slug, "public", issuerID)
	subject := urn.NewUserSubject(mockidp.MockUserID)
	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/x/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)

	call, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "ping", "arguments": map[string]any{}}})
	require.NoError(t, err)
	_, _ = servePublicHTTP(t, context.Background(), ti, slug, call, token, map[string]string{"User-Agent": "claude-code/2.1.0"})

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderServerError)
	require.Equal(t, mcpServer.ID.String(), row.ServerID)
	require.Equal(t, mcpconnectionattempts.FamilyClaudeCode, row.ClientFamily)
	require.Equal(t, string(mcpconnectionattempts.WhoProvider), row.WhoFixes)
}

// Denied mcp:connect on a proxied server is the admin's to grant.
func TestProxiedConnectDeniedRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(upstream.Close)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "endpoint-" + uuid.NewString()
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slug, "private", issuerID)
	subject := urn.NewUserSubject(mockidp.MockUserID)
	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/x/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)

	_, err = servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), token, nil)
	require.Error(t, err)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeNoServerAccess)
	require.Equal(t, mcpServer.ID.String(), row.ServerID)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

// A hosted tool the caller may not use is recorded, and the response stays the denial.
func TestHostedToolDeniedRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	addHTTPTools(t, ctx, ti, fx.toolset.ID, fx.target.ProjectID, fx.orgID, "deploy", "rollback")

	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	selector := authz.NewSelector(authz.ScopeMCPConnect, fx.target.MCPResourceID.String())
	selector[authz.SelectorKeyTool] = "rollback"
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: fx.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)
	session := seedWorkloadSession(t, ctx, ti, fx, urn.NewWorkloadSubject(issuerID, workloadSessionSubject))
	token := mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), token))
	w := serveMCPResponse(t, ti, slug, makeToolsCallBody("deploy"), token)
	require.Contains(t, w.Body.String(), mcpaccess.ToolPermissionDeniedMessage)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeToolPermissionDenied)
	require.Equal(t, fx.orgID, row.OrganizationID)
}

func TestGatewayMemberRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-rows-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(deny.Close)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-rows", "", []uuid.UUID{issuerID})
	remoteIssuerID := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", deny.URL, conv.ToNullUUID(remoteIssuerID), 0)
	subject := createTestUser(t, ctx, ti, "meta-rows-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)

	_, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidRemoteSession)
	require.Equal(t, meta.ID.String(), row.ServerID)
	require.NotEmpty(t, row.MemberID)
	require.Equal(t, subject.String(), row.SubjectURN)

	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-refused", deny.URL)
	_, isError = metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
}

// A busy or broken member must write its row and read true, through the real request path.
func TestGatewayMemberOutageRows(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		want   mcpconnectionattempts.Code
		text   string
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, want: mcpconnectionattempts.CodeProviderRateLimited, text: "rejected the call: rejected by transport"},
		{name: "server error", status: http.StatusInternalServerError, want: mcpconnectionattempts.CodeProviderServerError, text: "rejected the call: rejected by transport"},
		{name: "unavailable", status: http.StatusServiceUnavailable, want: mcpconnectionattempts.CodeProviderServerError, text: "rejected the call: rejected by transport"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

			issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
			metaSlug := "meta-outage-" + uuid.NewString()[:8]
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(upstream.Close)
			clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-outage", "", []uuid.UUID{issuerID})
			createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream.URL, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
			subject := createTestUser(t, ctx, ti, "meta-outage-user-"+uuid.NewString())
			bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
			insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-outage", upstream.URL)

			text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
			require.True(t, isError)
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, `server "member" `+tc.text+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
			require.NotEmpty(t, row.MemberID)
			require.Equal(t, string(mcpconnectionattempts.WhoProvider), row.WhoFixes)
		})
	}
}

// A gateway's rows name the client its session reported, with the User-Agent as the fallback.
func TestGatewayRowsReadTheReportedClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-client-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-client", "", []uuid.UUID{issuerID})
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream.URL, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
	subject := createTestUser(t, ctx, ti, "meta-client-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-client", upstream.URL)

	const userAgent = "python-httpx/0.28.1"
	resp, err := servePublicHTTP(t, context.Background(), ti, metaSlug, makeInitializeBodyWithClientInfo(t, "codex-mcp-client", "0.50.0"), bearer, map[string]string{"User-Agent": userAgent})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	sessionID := resp.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	call := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute_tool", "arguments": map[string]any{"name": "member--ping", "arguments": map[string]any{}}})
	_, err = servePublicHTTP(t, context.Background(), ti, metaSlug, call, bearer, map[string]string{"User-Agent": userAgent, "Mcp-Session-Id": sessionID})
	require.NoError(t, err)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderServerError)
	require.Equal(t, mcpconnectionattempts.FamilyCodexCLI, row.ClientFamily)
	require.Equal(t, "0.50.0", row.ClientVersion)

	// A session that reported an unknown name falls back, and so does none at all.
	resp, err = servePublicHTTP(t, context.Background(), ti, metaSlug, makeInitializeBodyWithClientInfo(t, "acme-agent", "9.9"), bearer, map[string]string{"User-Agent": userAgent})
	require.NoError(t, err)
	before := len(rec.Rows())
	_, err = servePublicHTTP(t, context.Background(), ti, metaSlug, call, bearer, map[string]string{"User-Agent": userAgent, "Mcp-Session-Id": resp.Header().Get("Mcp-Session-Id")})
	require.NoError(t, err)
	require.Greater(t, len(rec.Rows()), before, "the fallback call must record a refusal")
	for _, late := range rec.Rows()[before:] {
		require.Equal(t, mcpconnectionattempts.FamilyScript, late.ClientFamily)
		require.Equal(t, "0.28.1", late.ClientVersion)
	}
}

// A toolset gate's rejection reads the record a hosted session keeps, and a missing session the User-Agent.
func TestGateRejectionReadsTheReportedClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-reported")

	sessionID := uuid.NewString()
	require.NoError(t, newClientInfoStore(t).Store(ctx, fixture.Toolset.ProjectID, fixture.Toolset.Slug, sessionID, sessionclientinfo.Info{Name: "cursor-vscode", Version: "1.7.3"}, time.Now().UnixMilli()))

	subject := urn.NewUserSubject("gate-reported-" + uuid.NewString())
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)
	_, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, map[string]string{"User-Agent": "claude-code/2.1.0", "Mcp-Session-Id": sessionID})
	require.Error(t, err)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidRemoteSession)
	require.Equal(t, mcpconnectionattempts.FamilyCursor, row.ClientFamily)
	require.Equal(t, "1.7.3", row.ClientVersion)
	require.NotEmpty(t, row.AppKey)
}

// A gateway member that answers writes nothing, through the real request path.
func TestHealthyGatewayMemberRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	memberSlug := "member-remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", memberSlug, 1, upstream.url)

	envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{"name": memberSlug + "--ping", "arguments": map[string]any{}})
	require.NotContains(t, string(envelope["result"]), `"isError":true`)
	rec.RequireNothing()
}

// A row written the production way reaches ClickHouse, so the recorder is really wired to the service.
func TestRecorderWritesRowsToClickHouse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	recorder := mcpconnectionattempts.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, mcpconnectionattemptstest.LogsOn)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(cleanupCtx)
	})
	ti.service.SetConnectionRecorder(recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-wired")
	subject := urn.NewUserSubject("wired-user-" + uuid.NewString())
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	_, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Error(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, qerr := conn.Query(ctx, `SELECT reason FROM mcp_connection_events WHERE subject_urn = $1 AND is_final`, subject.String())
		if !assert.NoError(c, qerr) {
			return
		}
		defer o11y.NoLogDefer(rows.Close)
		var reasons []string
		for rows.Next() {
			var reason string
			assert.NoError(c, rows.Scan(&reason))
			reasons = append(reasons, reason)
		}
		assert.Contains(c, reasons, mcpconnectionattempts.CodeInvalidRemoteSession.String())
	}, 30*time.Second, 200*time.Millisecond)
}

// Denied mcp:connect on a hosted toolset is the admin's to grant.
func TestHostedConnectDeniedRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _ := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, "hosted-denied-"+uuid.NewString()[:8])
	bearer := mintUserSessionBearerForSubject(t, ti, toolset, urn.NewUserSubject(mockidp.MockUserID))

	_, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Error(t, err)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeNoServerAccess)
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

// A local member-proxy refusal already has its own connection row. The SDK's
// JSON-RPC wrapper must not add a second, misleading provider rejection.
func TestGatewayLocalUsageRefusalRecordsOnce(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth := requireProjectAuthContext(t, ctx)
	// Billing's stub persists by organization ID on disk, unlike the isolated
	// test databases. Give this cap fixture its own organization and project.
	orgID := "org-" + uuid.NewString()
	org, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Usage fixture", Slug: orgID})
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: org.ID, Name: "Usage fixture", Slug: "usage-fixture"})
	require.NoError(t, err)
	caller := *auth
	caller.ActiveOrganizationID, caller.ProjectID = org.ID, &project.ID
	auth = &caller
	ctx = contextvalues.SetAuthContext(ctx, auth)
	require.NoError(t, orgrepo.New(ti.conn).SetAccountType(ctx, orgrepo.SetAccountTypeParams{ID: auth.ActiveOrganizationID, GramAccountType: string(billing.TierBase)}))
	require.NoError(t, os.MkdirAll("scratch", 0750))
	usageFile := filepath.Join("scratch", "billingusage-"+auth.ActiveOrganizationID+".local.json")
	require.NoError(t, os.WriteFile(usageFile, []byte(`{"ToolCalls":2000,"IncludedToolCalls":1,"HasActiveSubscription":false}`), 0600))
	t.Cleanup(func() { require.NoError(t, os.Remove(usageFile)) })
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, meta.ID, "remote member", "member", 1, upstream.url)
	envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{"name": "member--ping", "arguments": map[string]any{}})
	text, isError := metaToolResultText(t, envelope)
	require.True(t, isError)
	require.Contains(t, text, "tool usage limit reached")
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeUsageLimitReached)
	require.Len(t, rec.Rows(), 1)
	for _, request := range upstream.journal() {
		require.NotEqual(t, "tools/call", request.rpcMethod)
	}
}

// Tool errors over successful HTTP or a non-auth HTTP 400 do not prove rejected
// credentials, regardless of the JSON-RPC error code. Preserve the tool response.
func TestGatewayToolErrorsRecordNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ code, status int }{
		{-32601, 200}, {-32602, 200}, {-32603, 200}, {-32000, 200}, {0, 200}, {-32602, 400}, {-32000, 400},
	} {
		code := tc.code
		t.Run(fmt.Sprintf("%d/%d", tc.status, code), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			auth := requireProjectAuthContext(t, ctx)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			slug := "meta-" + uuid.NewString()
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
			upstream := newRecordingUpstream(t, "ping")
			target, err := url.Parse(upstream.url)
			require.NoError(t, err)
			forward := httputil.NewSingleHostReverseProxy(target)
			var calls atomic.Int32
			failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					body, readErr := io.ReadAll(r.Body)
					if !assert.NoError(t, readErr) {
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
					var rpc struct {
						Method string          `json:"method"`
						ID     json.RawMessage `json:"id"`
					}
					if !assert.NoError(t, json.Unmarshal(body, &rpc)) {
						return
					}
					if rpc.Method == "tools/call" {
						calls.Add(1)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(tc.status)
						assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "error": map[string]any{"code": code, "message": "tool request failed"}}))
						return
					}
				}
				forward.ServeHTTP(w, r)
			}))
			t.Cleanup(failing.Close)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, meta.ID, "remote member", "member", 1, failing.URL)
			envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{"name": "member--ping", "arguments": map[string]any{}})
			text, isError := metaToolResultText(t, envelope)
			require.True(t, isError)
			if code == 0 {
				require.Contains(t, text, `server "member" rejected the call:`)
			} else {
				require.Equal(t, `server "member" rejected the call: tool request failed`, text)
			}
			require.Equal(t, int32(1), calls.Load())
			rec.RequireNothing()
		})
	}
}

func TestGatewayHostedMissingSecurityRecordsResponsibleParty(t *testing.T) {
	t.Parallel()
	for _, oauth := range []bool{false, true} {
		t.Run(fmt.Sprint(oauth), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			auth := requireProjectAuthContext(t, ctx)
			slug := "meta-" + uuid.NewString()
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
			member := seedHostedMetaMember(t, ctx, ti, meta.ID, "unconfigured", 0, mcpservers.VisibilityPublic)
			tool := "test_tool"
			want := mcpconnectionattempts.CodeCredentialMisconfigured
			who := mcpconnectionattempts.WhoAdmin
			if oauth {
				ti.addToolWithDualSecurity(ctx, t, member.toolsetID, *auth.ProjectID, auth.ActiveOrganizationID)
				tool = "dual_sec_tool"
				want, who = mcpconnectionattempts.CodeInvalidRemoteSession, mcpconnectionattempts.WhoPerson
			} else {
				ti.addToolWithSecurity(ctx, t, member.toolsetID, *auth.ProjectID, auth.ActiveOrganizationID)
			}
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			rpc := executeMetaTool(t, ti, slug, "", member.slug+"--"+tool)
			text, isError := metaToolResultText(t, rpc)
			require.True(t, isError)
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, want)
			require.Equal(t, fmt.Sprintf("server %q requires authentication that this meta MCP session does not satisfy", member.slug)+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
			require.Equal(t, string(who), row.WhoFixes)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

func TestGatewayUnknownCachedToolRecordsNothing(t *testing.T) {
	t.Parallel()
	ctx, g := newStaleChoiceGateway(t, toolfilter.CatalogModeCached, metamcp.DiscoveryModeProgressive)
	connection := g.approve(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	g.ti.service.SetConnectionRecorder(rec.Recorder)
	for _, name := range []string{"source--does_not_exist", "nonexistent--tool_000"} {
		response := g.call(t, connection.token, name)
		require.Contains(t, response.Body.String(), remotemcp.ToolNotApprovedMessage)
	}
	rec.RequireNothing()
}

func TestFreshRefreshSuppressesOnlyDuplicateGateRow(t *testing.T) {
	t.Parallel()
	rec := mcpconnectionattemptstest.New(t)
	ctx, ti := buildTestMCPService(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, nil, false, mcp.MetaRuntimeConfig{}, testenv.NewTracerProvider(t), nil, nil, "", false, rec.Recorder)
	ti.service.SetConnectionRecorder(rec.Recorder)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "fresh-outage")
	subject := urn.NewUserSubject("gate-user-" + uuid.NewString())
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject)
	var calls atomic.Int32
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)
	first, freshErr := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Error(t, freshErr)
	rows := rec.Rows()
	require.Len(t, rows, 1)
	require.Equal(t, mcpconnectionattempts.StepProviderRefresh.String(), rows[0].Step)
	second, cachedErr := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.EqualError(t, cachedErr, freshErr.Error())
	require.Equal(t, first.Code, second.Code)
	require.Equal(t, first.Body.String(), second.Body.String())
	require.EqualValues(t, 1, calls.Load())
	rows = rec.Rows()
	require.Len(t, rows, 2)
	require.Equal(t, mcpconnectionattempts.StepMCPRequest.String(), rows[1].Step)
}

func TestFreshMisconfiguredRefreshWritesOnlyTheRefreshRow(t *testing.T) {
	t.Parallel()
	rec := mcpconnectionattemptstest.New(t)
	ctx, ti := buildTestMCPService(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, nil, false, mcp.MetaRuntimeConfig{}, testenv.NewTracerProvider(t), nil, nil, "", false, rec.Recorder)
	ti.service.SetConnectionRecorder(rec.Recorder)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "fresh-misconfigured")
	subject := urn.NewUserSubject("gate-user-" + uuid.NewString())
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject)
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)
	_, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Error(t, err)
	rows := rec.Rows()
	require.Len(t, rows, 1)
	require.Equal(t, mcpconnectionattempts.StepProviderRefresh.String(), rows[0].Step)
}

func TestDirectHostedMissingSecurityRecordsResponsibleParty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		oauth      bool
		providedBy string
		want       mcpconnectionattempts.Code
		who        mcpconnectionattempts.WhoFixes
	}{
		{name: "no config", want: mcpconnectionattempts.CodeCredentialMisconfigured, who: mcpconnectionattempts.WhoAdmin},
		{name: "system provided", providedBy: "system", want: mcpconnectionattempts.CodeCredentialMisconfigured, who: mcpconnectionattempts.WhoAdmin},
		{name: "user provided", providedBy: "user", want: mcpconnectionattempts.CodeCallerCredentialMissing, who: mcpconnectionattempts.WhoPerson},
		{name: "oauth alternative", oauth: true, want: mcpconnectionattempts.CodeInvalidRemoteSession, who: mcpconnectionattempts.WhoPerson},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			auth := requireProjectAuthContext(t, ctx)
			ts := createPublicMCPToolset(t, ctx, ti, auth, "direct-security-"+uuid.NewString()[:8])
			if tc.oauth {
				ti.addToolWithDualSecurity(ctx, t, ts.ID, *auth.ProjectID, auth.ActiveOrganizationID)
			} else {
				ti.addToolWithSecurity(ctx, t, ts.ID, *auth.ProjectID, auth.ActiveOrganizationID)
			}
			if tc.providedBy != "" {
				setSecurityVariableProvidedBy(t, ctx, ti, ts.ID, *auth.ProjectID, tc.providedBy)
			}
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			response, err := servePublicHTTP(t, context.Background(), ti, ts.McpSlug.String, makeInitializeBody(), "", nil)
			require.EqualError(t, err, "serve public: "+oops.C(oops.CodeUnauthorized).Error())
			require.Empty(t, response.Body.String())
			require.Empty(t, response.Header().Get("WWW-Authenticate"))
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// A notification gets the 401 but writes no row; a tool call missing the caller's credentials does.
func TestDirectHostedMissingSecuritySkipsNotifications(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth := requireProjectAuthContext(t, ctx)
	ts := createPublicMCPToolset(t, ctx, ti, auth, "direct-security-"+uuid.NewString()[:8])
	ti.addToolWithSecurity(ctx, t, ts.ID, *auth.ProjectID, auth.ActiveOrganizationID)
	setSecurityVariableProvidedBy(t, ctx, ti, ts.ID, *auth.ProjectID, "user")
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	_, err := servePublicHTTP(t, t.Context(), ti, ts.McpSlug.String, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), "", nil)
	require.EqualError(t, err, "serve public: "+oops.C(oops.CodeUnauthorized).Error())
	rec.RequireNothing()

	_, err = servePublicHTTP(t, t.Context(), ti, ts.McpSlug.String, makeToolsCallBody("test_tool"), "", nil)
	require.EqualError(t, err, "serve public: "+oops.C(oops.CodeUnauthorized).Error())
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeCallerCredentialMissing)
	require.Len(t, rec.Rows(), 1)
}

// Our own failure during the security check writes a row naming us.
func TestDirectHostedSecurityCheckFailureRecordsInternalError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth := requireProjectAuthContext(t, ctx)
	ts := createPublicMCPToolset(t, ctx, ti, auth, "direct-security-"+uuid.NewString()[:8])
	ti.addToolWithSecurity(ctx, t, ts.ID, *auth.ProjectID, auth.ActiveOrganizationID)
	key := ti.createTestAPIKey(ctx, t)
	ti.service.SetEnvironmentLoader(failingSystemEnv{})
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	_, err := servePublicHTTP(t, t.Context(), ti, ts.McpSlug.String, makeToolsCallBody("test_tool"), key, nil)
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, err)
	require.Equal(t, oops.CodeUnexpected, shareable.Code)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInternalError)
	require.Equal(t, string(mcpconnectionattempts.WhoSpeakeasy), row.WhoFixes)
	require.Len(t, rec.Rows(), 1)
}

// A gateway member's security check failing on our side writes one row for that member.
func TestGatewayHostedSecurityCheckFailureRecordsInternalError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth := requireProjectAuthContext(t, ctx)
	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "unconfigured", 0, mcpservers.VisibilityPublic)
	ti.addToolWithSecurity(ctx, t, member.toolsetID, *auth.ProjectID, auth.ActiveOrganizationID)
	ti.service.SetEnvironmentLoader(failingSystemEnv{})
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	body := makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "execute_tool",
		"arguments": map[string]any{"name": member.slug + "--test_tool", "arguments": map[string]any{}},
	})
	resp, err := servePublicHTTP(t, t.Context(), ti, slug, body, "", nil)
	require.NoError(t, err)
	require.Contains(t, resp.Body.String(), `"code":-32603`)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInternalError)
	require.Equal(t, member.serverID.String(), row.MemberID)
	require.Len(t, rec.Rows(), 1)
}

// failingSystemEnv fails the security check's system environment read.
type failingSystemEnv struct{ toolconfig.EnvironmentLoader }

func (failingSystemEnv) LoadSystemEnv(context.Context, uuid.UUID, uuid.UUID, string, string) (*toolconfig.CaseInsensitiveEnv, error) {
	return nil, errors.New("environment store unavailable")
}

func setSecurityVariableProvidedBy(t *testing.T, ctx context.Context, ti *testInstance, toolsetID, projectID uuid.UUID, providedBy string) {
	t.Helper()
	queries := metadatarepo.New(ti.conn)
	metadata, err := queries.UpsertMetadataByMcpServerID(ctx, metadatarepo.UpsertMetadataByMcpServerIDParams{McpServerID: toolsetID, ProjectID: projectID})
	require.NoError(t, err)
	_, err = queries.UpsertEnvironmentConfig(ctx, metadatarepo.UpsertEnvironmentConfigParams{ProjectID: projectID, McpMetadataID: metadata.ID, VariableName: "TEST_API_KEY", ProvidedBy: providedBy})
	require.NoError(t, err)
}

// An external OAuth server's first handshake carries no token, and its 401 is how sign-in starts.
func TestExternalOAuthFirstHandshakeRecordsNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		add  func(context.Context, *testing.T, *testInstance, uuid.UUID, *contextvalues.AuthContext)
	}{
		{name: "no schemes"},
		{name: "api key scheme", add: func(ctx context.Context, t *testing.T, ti *testInstance, toolset uuid.UUID, auth *contextvalues.AuthContext) {
			t.Helper()
			ti.addToolWithSecurity(ctx, t, toolset, *auth.ProjectID, auth.ActiveOrganizationID)
		}},
		{name: "oauth scheme", add: func(ctx context.Context, t *testing.T, ti *testInstance, toolset uuid.UUID, auth *contextvalues.AuthContext) {
			t.Helper()
			ti.addToolWithDualSecurity(ctx, t, toolset, *auth.ProjectID, auth.ActiveOrganizationID)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			auth := requireProjectAuthContext(t, ctx)
			external := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, auth, oauthtest.ExternalOAuthToolsetOpts{Slug: "first-handshake", IsPublic: true})
			if tc.add != nil {
				tc.add(ctx, t, ti, external.Toolset.ID, auth)
			}
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)

			for range 2 {
				response, err := servePublicHTTP(t, context.Background(), ti, external.McpSlug, makeInitializeBody(), "", nil)
				require.EqualError(t, err, "serve public: "+oops.C(oops.CodeUnauthorized).Error())
				require.Contains(t, response.Header().Get("WWW-Authenticate"), "resource_metadata=")
			}
			rec.RequireNothing()
		})
	}
}

// Only the tokenless start of sign-in is silent: a token that still falls short is a failure.
func TestExternalOAuthTokenWithUnmetSchemeRecords(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth := requireProjectAuthContext(t, ctx)
	external := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, auth, oauthtest.ExternalOAuthToolsetOpts{Slug: "token-unmet", IsPublic: true})
	ti.addToolWithSecurity(ctx, t, external.Toolset.ID, *auth.ProjectID, auth.ActiveOrganizationID)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	response, err := servePublicHTTP(t, context.Background(), ti, external.McpSlug, makeInitializeBody(), "external-token", nil)
	require.Error(t, err)
	require.Contains(t, response.Header().Get("WWW-Authenticate"), "resource_metadata=")
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeCredentialMisconfigured)
}

// A later client needing a re-link decides the response, so it keeps its own row beside the refresh row.
func TestFreshRefreshOutrankedByReconnectKeepsTheReconnectRow(t *testing.T) {
	t.Parallel()
	rec := mcpconnectionattemptstest.New(t)
	ctx, ti := buildTestMCPService(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, nil, false, mcp.MetaRuntimeConfig{}, testenv.NewTracerProvider(t), nil, nil, "", false, rec.Recorder)
	ti.service.SetConnectionRecorder(rec.Recorder)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "fresh-outranked")
	// Clients resolve in id order, so the lower id must be the one that refreshes.
	refreshing := fixture.RemoteSessionClient.ID
	other := createConsentRemoteClient(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "fresh-outranked-unlinked-"+uuid.NewString()[:8], "", []uuid.UUID{fixture.UserSessionIssuer.ID})
	if other.String() < refreshing.String() {
		refreshing = other
	}

	neverLinked := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, urn.NewUserSubject("gate-user-"+uuid.NewString()))
	want, wantErr := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), neverLinked, nil)
	require.Error(t, wantErr)
	require.Len(t, rec.Rows(), 1, "a never-linked subject gets the gate row")

	subject := urn.NewUserSubject("gate-user-" + uuid.NewString())
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, refreshing, subject)
	var calls atomic.Int32
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, refreshing, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	got, gotErr := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject), nil)
	require.EqualValues(t, 1, calls.Load(), "the refreshing client must resolve first")
	require.EqualError(t, gotErr, wantErr.Error())
	require.Equal(t, want.Code, got.Code)
	require.Equal(t, want.Body.String(), got.Body.String())
	require.Equal(t, want.Header().Get("WWW-Authenticate"), got.Header().Get("WWW-Authenticate"))

	rows := rec.Rows()[1:]
	require.Len(t, rows, 2)
	require.Equal(t, mcpconnectionattempts.StepProviderRefresh.String(), rows[0].Step)
	require.Equal(t, mcpconnectionattempts.StepMCPRequest.String(), rows[1].Step)
	require.Equal(t, mcpconnectionattempts.CodeInvalidRemoteSession.String(), rows[1].Reason)
}

// A gateway call is past sign-in and takes no caller headers, so its rows stay as they were.
func TestGatewayMemberSecurityOutcomeIgnoresDirectRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		external   bool
		schemes    bool
		providedBy string
		want       mcpconnectionattempts.Code
	}{
		{name: "external oauth without schemes", external: true, want: mcpconnectionattempts.CodeInvalidRemoteSession},
		{name: "external oauth with api key", external: true, schemes: true, want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "user provided variable", schemes: true, providedBy: "user", want: mcpconnectionattempts.CodeCredentialMisconfigured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			auth := requireProjectAuthContext(t, ctx)
			slug := "meta-" + uuid.NewString()
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
			tools := []string{"ping"}
			tool := "ping"
			if tc.schemes {
				tools, tool = nil, "test_tool"
			}
			member := seedHostedMetaMember(t, ctx, ti, meta.ID, "unconfigured", 0, mcpservers.VisibilityPublic, tools...)
			if tc.schemes {
				ti.addToolWithSecurity(ctx, t, member.toolsetID, *auth.ProjectID, auth.ActiveOrganizationID)
			}
			if tc.providedBy != "" {
				setSecurityVariableProvidedBy(t, ctx, ti, member.toolsetID, *auth.ProjectID, tc.providedBy)
			}
			if tc.external {
				external := oauthtest.CreateExternalOAuthToolset(t, ctx, ti.conn, auth, oauthtest.ExternalOAuthToolsetOpts{Slug: "gateway-member", IsPublic: true})
				toolset, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: member.toolsetID, ProjectID: *auth.ProjectID})
				require.NoError(t, err)
				_, err = toolsetsrepo.New(ti.conn).UpdateToolsetExternalOAuthServer(ctx, toolsetsrepo.UpdateToolsetExternalOAuthServerParams{
					ExternalOauthServerID: uuid.NullUUID{UUID: external.ServerMetadata.ID, Valid: true},
					Slug:                  toolset.Slug,
					ProjectID:             toolset.ProjectID,
				})
				require.NoError(t, err)
			}
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			text, isError := metaToolResultText(t, executeMetaTool(t, ti, slug, "", member.slug+"--"+tool))
			require.True(t, isError)
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, fmt.Sprintf("server %q requires authentication that this meta MCP session does not satisfy", member.slug)+contextvalues.ConnectionReferenceMarker+row.AttemptID.String(), text)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// A refused refresh wrote its row, so the gate adds none, and the caller still gets the reconnect challenge.
func TestFreshInvalidGrantWritesOnlyTheRefreshRow(t *testing.T) {
	t.Parallel()
	rec := mcpconnectionattemptstest.New(t)
	ctx, ti := buildTestMCPService(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{}, nil, nil, false, mcp.MetaRuntimeConfig{}, testenv.NewTracerProvider(t), nil, nil, "", false, rec.Recorder)
	ti.service.SetConnectionRecorder(rec.Recorder)
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "fresh-invalid-grant")

	neverLinked := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, urn.NewUserSubject("gate-user-"+uuid.NewString()))
	want, wantErr := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), neverLinked, nil)
	challenge, ok := errors.AsType[*oops.ShareableError](wantErr)
	require.True(t, ok, wantErr)
	require.Equal(t, oops.CodeUnauthorized, challenge.Code)
	require.Contains(t, want.Header().Get("WWW-Authenticate"), "resource_metadata=")
	require.Len(t, rec.Rows(), 1, "a never-linked subject gets the gate row")

	subject := urn.NewUserSubject("gate-user-" + uuid.NewString())
	upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject)
	pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	got, gotErr := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject), nil)
	require.EqualError(t, gotErr, wantErr.Error())
	require.Equal(t, want.Code, got.Code)
	require.Equal(t, want.Body.String(), got.Body.String())
	require.Equal(t, want.Header().Get("WWW-Authenticate"), got.Header().Get("WWW-Authenticate"))

	rows := rec.Rows()[1:]
	require.Len(t, rows, 1)
	require.Equal(t, mcpconnectionattempts.StepProviderRefresh.String(), rows[0].Step)
}

// upsertUnreadableRemoteSession stores a live connection whose access token can't be decrypted.
func upsertUnreadableRemoteSession(t *testing.T, ctx context.Context, ti *testInstance, userSessionIssuerID, remoteSessionClientID uuid.UUID, subject urn.SessionSubject, resource string) {
	t.Helper()

	_, err := remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userSessionIssuerID,
		RemoteSessionClientID: remoteSessionClientID,
		AccessTokenEncrypted:  "not-ciphertext",
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: pgtype.Text{String: "", Valid: false},
		RefreshExpiresAt:      pgtype.Timestamptz{Valid: false},
		Scopes:                []string{},
		Resource:              pgtype.Text{String: resource, Valid: resource != ""},
	})
	require.NoError(t, err)
}

// With nothing stored readable, a direct server asks the person to reconnect and records why.
func TestUnreadableUpstreamTokenDirectServerAsksToReconnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-unreadable")

	subject := urn.NewUserSubject("gate-unreadable-user-" + uuid.NewString())
	upsertUnreadableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, "")
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Equal(t, reconnectDescription, requireOopsCode(t, err, oops.CodeUnauthorized))
	require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStoredSignInUnreadable)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
	require.Equal(t, subject.String(), row.SubjectURN)
	stored, err := remotesessions_repo.New(ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: fixture.RemoteSessionClient.ID})
	require.NoError(t, err)
	require.Equal(t, stored.ID.String(), row.RemoteSessionID)
}

// With nothing stored readable, a gateway never calls the member without credentials and asks for a reconnect.
func TestUnreadableUpstreamTokenGatewayMemberAsksToReconnect(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		resource bool
	}{
		{name: "grant names the member", resource: true},
		{name: "grant names no resource", resource: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

			var memberCalls atomic.Int32
			// Answers anyone, so an anonymous call would succeed.
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					memberCalls.Add(1)
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(upstream.Close)

			issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
			metaSlug := "meta-unreadable-" + uuid.NewString()[:8]
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
			clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-unreadable", "", []uuid.UUID{issuerID})
			createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream.URL, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
			subject := createTestUser(t, ctx, ti, "meta-unreadable-user-"+uuid.NewString())
			bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
			resource := ""
			if tc.resource {
				resource = upstream.URL
			}
			upsertUnreadableRemoteSession(t, ctx, ti, issuerID, clientID, subject, resource)

			text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
			require.True(t, isError)
			require.Contains(t, text, `server "member" can't use its saved sign-in anymore. Ask the user to open `)
			require.Contains(t, text, "to reconnect it, then retry")
			require.NotContains(t, strings.ToLower(text), "retry shortly")
			require.Zero(t, memberCalls.Load(), "never an anonymous call")

			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStoredSignInUnreadable)
			require.Equal(t, meta.ID.String(), row.ServerID)
			require.NotEmpty(t, row.MemberID)
			require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
			stored, err := remotesessions_repo.New(ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientID})
			require.NoError(t, err)
			require.Equal(t, stored.ID.String(), row.RemoteSessionID)

			body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolConnectionStatus, "arguments": map[string]any{}})
			resp, err := servePublicHTTP(t, ctx, ti, metaSlug, body, bearer, nil)
			require.NoError(t, err)
			var status struct {
				StructuredContent metamcp.ConnectionStatusResult `json:"structuredContent"`
			}
			require.NoError(t, json.Unmarshal(decodeRPCResponse(t, resp)["result"], &status))
			require.Len(t, status.StructuredContent.Servers, 1)
			require.Equal(t, metamcp.SignInNotConnected, status.StructuredContent.Servers[0].Status)
			require.Nil(t, status.StructuredContent.Servers[0].Outage, "not an outage to wait out")
			require.Zero(t, memberCalls.Load())
		})
	}
}

// overwriteMemberTokens replaces the fixture's stored pair; an empty plaintext stores ciphertext the key can't read.
func overwriteMemberTokens(t *testing.T, ctx context.Context, f memberRefreshOutageFixture, access, refresh string, accessExpiresIn time.Duration) {
	t.Helper()
	encrypt := func(plain string) string {
		if plain == "" {
			return "not-ciphertext"
		}
		ciphertext, err := f.ti.enc.Encrypt([]byte(plain))
		require.NoError(t, err)
		return ciphertext
	}
	current, err := remotesessions_repo.New(f.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: f.subject, RemoteSessionClientID: f.clientID})
	require.NoError(t, err)
	_, err = remotesessions_repo.New(f.ti.conn).UpdateRemoteSessionTokensIfUnchanged(ctx, remotesessions_repo.UpdateRemoteSessionTokensIfUnchangedParams{
		SubjectUrn: f.subject, RemoteSessionClientID: f.clientID, ExpectedUpdatedAt: current.UpdatedAt,
		AccessTokenEncrypted: encrypt(access), AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(accessExpiresIn)),
		RefreshTokenEncrypted: conv.ToPGText(encrypt(refresh)), Scopes: current.Scopes,
	})
	require.NoError(t, err)
}

// A gateway member whose access token can't be read is renewed during the call and dialed with the new token.
func TestUnreadableUpstreamTokenGatewayMemberHeals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	overwriteMemberTokens(t, ctx, f, "", "live-refresh", time.Hour)

	_, _ = metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
	require.EqualValues(t, 1, f.refreshes.Load())
	require.Positive(t, f.memberCalls.Load())
	require.Equal(t, "Bearer refreshed-token", f.upstream.Load())

	stored, err := remotesessions_repo.New(ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: f.subject, RemoteSessionClientID: f.clientID})
	require.NoError(t, err)
	access, err := ti.enc.Decrypt(stored.AccessTokenEncrypted)
	require.NoError(t, err, "the renewed access token is stored under the current key")
	require.Equal(t, "refreshed-token", access)
	refresh, err := ti.enc.Decrypt(stored.RefreshTokenEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "rotated-refresh", refresh)
}

// An expired access token with an unreadable refresh token never reaches the member without credentials.
func TestUnreadableUpstreamRefreshTokenGatewayMemberAsksToReconnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	overwriteMemberTokens(t, ctx, f, "stale-access", "", -time.Hour)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, `server "member" can't use its saved sign-in anymore`)
	require.Zero(t, f.memberCalls.Load(), "never an anonymous call")
	require.Zero(t, f.refreshes.Load(), "an unreadable refresh token is never sent")
}

// A gateway member whose unreadable access token can't be renewed for a configuration fault names the administrator and is never dialed.
func TestUnreadableUpstreamTokenGatewayMemberMisconfiguredNamesAdministrator(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	var refreshes atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"unauthorized_client"}`)
	}))
	t.Cleanup(provider.Close)
	var memberCalls atomic.Int32
	// Answers anyone, so an anonymous call would succeed.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			memberCalls.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-unreadable-admin-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-unreadable-admin", provider.URL, []uuid.UUID{issuerID})
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream.URL, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
	subject := createTestUser(t, ctx, ti, "meta-unreadable-admin-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
	refresh, err := ti.enc.Encrypt([]byte("live-refresh"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn: subject, UserSessionIssuerID: issuerID, RemoteSessionClientID: clientID,
		AccessTokenEncrypted: "not-ciphertext", AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.ToPGText(refresh), RefreshExpiresAt: pgtype.Timestamptz{Valid: false},
		Scopes: []string{}, Resource: pgtype.Text{String: upstream.URL, Valid: true},
	})
	require.NoError(t, err)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, `server "member" has an upstream credential that is misconfigured or was rejected.`)
	require.Contains(t, text, "Ask an admin of the server's project")
	require.Zero(t, memberCalls.Load(), "never an anonymous call")
	require.Positive(t, refreshes.Load())

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeCredentialMisconfigured)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
	require.NotEmpty(t, row.MemberID)
}

// A held grant on the member's own issuer answers for the member after its upstream URL moved, never an anonymous call.
func TestHeldGrantForMovedMemberURLIsNeverAnonymous(t *testing.T) {
	t.Parallel()

	const oldURL = "https://old.example.com/mcp"
	t.Run("outage", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPService(t)
		f := newMemberRefreshOutageFixture(t, ctx, ti, func(string) string { return oldURL })
		f.down.Store(true)

		text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
		require.True(t, isError)
		require.Contains(t, text, "Retry")
		require.Zero(t, f.memberCalls.Load(), "an outage never turns into an anonymous call")
	})
	t.Run("unreadable", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPService(t)
		f := newMemberRefreshOutageFixture(t, ctx, ti, func(string) string { return oldURL })
		overwriteMemberTokens(t, ctx, f, "", "", time.Hour)

		text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
		require.True(t, isError)
		require.Contains(t, text, `server "member" can't use its saved sign-in anymore`)
		require.Zero(t, f.memberCalls.Load())
	})
}

// Two members on one issuer share its one grant: an unreadable grant asks each to reconnect, and neither is called without it.
func TestUnreadableSharedIssuerGrantNeverDialsEitherMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	var calls atomic.Int32
	newUpstream := func() *httptest.Server {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				calls.Add(1)
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(upstream.Close)
		return upstream
	}
	upstreamA, upstreamB := newUpstream(), newUpstream()

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-shared-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-shared", "", []uuid.UUID{issuerID})
	remoteIssuer := conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID))
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "alpha", upstreamA.URL, remoteIssuer, 0)
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "beta", upstreamB.URL, remoteIssuer, 1)
	subject := createTestUser(t, ctx, ti, "meta-shared-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
	upsertUnreadableRemoteSession(t, ctx, ti, issuerID, clientID, subject, upstreamA.URL)

	for _, member := range []string{"alpha", "beta"} {
		text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, member+"--ping"))
		require.True(t, isError, member)
		require.Contains(t, text, `server "`+member+`" can't use its saved sign-in anymore`)
	}
	require.Zero(t, calls.Load(), "neither member is called without credentials")
}
