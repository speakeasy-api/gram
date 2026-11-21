package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// servedOn returns the served connections with or without a member.
func servedOn(rec *mcpconnectionattemptstest.Recording, member bool) []mcpconnectionattempts.Served {
	var out []mcpconnectionattempts.Served
	for _, served := range rec.Served() {
		if (served.MemberID != "") == member {
			out = append(out, served)
		}
	}
	return out
}

// A direct server's served request names the connection its failure rows name, and its server answered.
func TestDirectServedRecordsTheConnection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"content":[]}}`))
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
	resp, err := servePublicHTTP(t, context.Background(), ti, slug, call, token, map[string]string{"User-Agent": "claude-code/2.1.0"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	served := rec.RequireServed(true)
	rec.RequireNothing()
	require.Equal(t, mcpServer.ID.String(), served.ServerID)
	require.Equal(t, subject.String(), served.SubjectURN)
	require.Equal(t, mcpconnectionattempts.FamilyClaudeCode, served.ClientFamily)
	require.Empty(t, served.MemberID)
	require.Empty(t, served.RemoteSessionClientID, "no stored sign-in to the service was sent")
}

// A hosted server records each served request; a tool call counts only when its backend answered without an error.
func TestHostedServedOnlyWhenTheBackendAnswered(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		status   int
		answered bool
	}{
		{name: "backend answered", status: http.StatusOK, answered: true},
		{name: "backend failed", status: http.StatusInternalServerError, answered: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)

			var called atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called.Store(true)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"pong":true}`))
			}))
			t.Cleanup(upstream.Close)

			fixture := createIssuerGatedHTTPToolFixture(t, ctx, ti, authCtx, "served-"+uuid.NewString()[:8], upstream.URL)
			subject := urn.NewUserSubject("served-user-" + uuid.NewString())
			insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, "valid-upstream-token", time.Now().Add(time.Hour))
			bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

			resp, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.Code)
			initialize := rec.RequireServed(false)
			require.Equal(t, subject.String(), initialize.SubjectURN)

			call, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": fixture.ToolName, "arguments": map[string]any{}}})
			require.NoError(t, err)
			resp, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, call, bearer, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			require.True(t, called.Load())

			served := rec.Served()
			if !tc.answered {
				require.Len(t, served, 1, "a call whose backend failed is not served")
				return
			}
			require.Len(t, served, 2)
			callServed := served[1]
			require.Equal(t, tc.answered, !callServed.AnsweredAt.IsZero())
			require.Equal(t, initialize.ServerKind, callServed.ServerKind)
			require.Equal(t, initialize.ServerID, callServed.ServerID)
			require.Equal(t, initialize.SubjectURN, callServed.SubjectURN)
			require.Equal(t, initialize.AppKey, callServed.AppKey)
			require.Empty(t, callServed.RemoteSessionClientID, "a hosted call does not name the service client")
		})
	}
}

// A hosted request with no named person records nothing.
func TestHostedServedNeedsAPerson(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "served-public-"+uuid.NewString()[:8])

	resp, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code)
	rec.RequireNoneServed()
}

// A gateway member call its server answered records the member's connection with the person's own sign-in,
// and the gateway's own connection as answered by Speakeasy.
func TestGatewayMemberServedRecordsTheConnection(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-served-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	upstream := newRecordingUpstream(t, "ping")
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-served", "", []uuid.UUID{issuerID})
	member := createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream.url, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
	subject := createTestUser(t, ctx, ti, "meta-served-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-served", upstream.url)

	_, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.False(t, isError)
	rec.RequireNothing()

	members := servedOn(rec, true)
	require.Len(t, members, 1)
	require.Equal(t, meta.ID.String(), members[0].ServerID)
	require.Equal(t, member.mcpServerID.String(), members[0].MemberID)
	require.Equal(t, subject.String(), members[0].SubjectURN)
	require.False(t, members[0].AnsweredAt.IsZero(), "the member's server answered")
	require.Equal(t, clientID.String(), members[0].RemoteSessionClientID, "the person's own sign-in was sent")

	gateway := servedOn(rec, false)
	require.Len(t, gateway, 1)
	require.Equal(t, meta.ID.String(), gateway[0].ServerID)
	require.True(t, gateway[0].AnsweredAt.IsZero(), "Speakeasy answered the gateway request")
	require.Empty(t, gateway[0].RemoteSessionClientID)
}

// A member the person could not reach records a failure with their sign-in and no member served row.
func TestGatewayMemberFailureNamesTheServiceSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-refused-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(deny.Close)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-refused", "", []uuid.UUID{issuerID})
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", deny.URL, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
	subject := createTestUser(t, ctx, ti, "meta-refused-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-refused", deny.URL)

	_, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
	require.Equal(t, clientID.String(), row.RemoteSessionClientID)
	require.Empty(t, servedOn(rec, true), "a failed member call is not served")
	require.Empty(t, servedOn(rec, false), "nor is the gateway request that carried it")

	resp, err := servePublicHTTP(t, context.Background(), ti, metaSlug, makeMetaRPCBody(t, "tools/list", map[string]any{}), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	gateway := servedOn(rec, false)
	require.Len(t, gateway, 1, "the gateway's own method is served")
	require.Equal(t, subject.String(), gateway[0].SubjectURN)
}

// A member's own error result is not served, on the member or the gateway.
func TestGatewayMemberErrorResultIsNotServed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-tool-error-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	upstream := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: []testmcp.Tool{{
		Name:        "ping",
		Description: "fails",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Response:    testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "tool failed"}}, IsError: true},
	}}})
	t.Cleanup(upstream.Close)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tool-error", "", []uuid.UUID{issuerID})
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream.URL, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)), 0)
	subject := createTestUser(t, ctx, ti, "meta-tool-error-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)
	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-tool-error", upstream.URL)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.Contains(t, text, "tool failed")
	require.True(t, isError)
	require.Empty(t, servedOn(rec, true))
	require.Empty(t, servedOn(rec, false))
}
