// remotesession_resolver_integration_test.go drives an end-to-end
// tools/call against an issuer-gated toolset fronting an external MCP
// server. It pins the load-bearing claim of the resolver contract: the
// upstream MCP request receives `Authorization: Bearer <stored token>`
// for the resolved remote_sessions row — proving the exchanged token is
// what reaches the wire, not the user-session JWT, not an empty header,
// not some other row's token.
//
// Intentionally there is no upstream IDP token endpoint stood up: the
// resolver must short-circuit ValidateAndExchange when the stored
// access token is still valid. If a future implementation tries to
// refresh on every call, this test fails with a network error rather
// than passing silently.
package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcp_repo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestServePublic_UserSessionIssuerRemoteSessionResolvedTokenReachesExternalMCP(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mockTools := []testmcp.Tool{{
		Name:        "ping",
		Description: "Returns pong",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Response: testmcp.ToolResponse{
			Content: []map[string]any{{"type": "text", "text": "pong"}},
		},
	}}
	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	var capturedAuth atomic.Value
	target, err := url.Parse(mockServer.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			capturedAuth.Store(auth)
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(recorder.Close)

	fixture := createIssuerGatedExternalMCPFixture(t, ctx, ti, authCtx, "resolver-extmcp", recorder.URL)

	requestSubject := urn.NewUserSubject("resolver-user-" + uuid.NewString())
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, requestSubject, "valid-upstream-token", time.Now().Add(time.Hour))

	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, requestSubject)

	initBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
		},
	})
	require.NoError(t, err)
	initResp, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, initBody, sessionToken, nil)
	require.NoError(t, err, "initialize must succeed once the resolver supplies the upstream token")
	require.Equal(t, http.StatusOK, initResp.Code, "initialize response: %s", initResp.Body.String())

	callBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      fixture.ToolName,
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	callResp, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, callBody, sessionToken, nil)
	require.NoError(t, err, "tools/call must succeed once the resolver supplies the upstream token")
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call response: %s", callResp.Body.String())

	got, _ := capturedAuth.Load().(string)
	require.Equal(t, "Bearer valid-upstream-token", got,
		"resolver must forward the exchanged remote_session access token verbatim as the upstream Authorization header")
}

type issuerGatedExternalMCPFixture struct {
	Toolset             toolsets_repo.Toolset
	UserSessionIssuer   usersessions_repo.UserSessionIssuer
	RemoteSessionClient remotesessions_repo.RemoteSessionClient
	ToolName            string
}

// createIssuerGatedExternalMCPFixture wires a public, issuer-gated
// toolset to an external MCP attachment that requires OAuth, plus a
// remote_session_client bound to the same user_session_issuer so the
// resolver has a remote-session requirement to satisfy. The upstream
// URL is whatever the test wants to point at (typically a recording
// reverse proxy in front of a mock MCP server).
func createIssuerGatedExternalMCPFixture(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
	slugPrefix string,
	upstreamURL string,
) issuerGatedExternalMCPFixture {
	t.Helper()

	suffix := uuid.NewString()[:8]
	slug := slugPrefix + "-" + suffix

	userIssuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               "resolver-extmcp-usi-" + suffix,
		AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{
			Microseconds: int64(time.Hour / time.Microsecond),
			Days:         0,
			Months:       0,
			Valid:        true,
		},
	})
	require.NoError(t, err)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, slug)
	toolset, err = toolsetsRepo.UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           toolset.ProjectID,
	})
	require.NoError(t, err)

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      toolset.ProjectID,
		OrganizationID: toolset.OrganizationID,
		UserID:         "test-user",
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))

	externalmcpRepo := externalmcp_repo.New(ti.conn)
	registryID, err := externalmcpRepo.CreateMCPRegistry(ctx, externalmcp_repo.CreateMCPRegistryParams{
		Name: "resolver-extmcp-registry-" + suffix,
		Url:  upstreamURL,
	})
	require.NoError(t, err)
	attachment, err := externalmcpRepo.CreateExternalMCPAttachment(ctx, externalmcp_repo.CreateExternalMCPAttachmentParams{
		DeploymentID:            deploymentID,
		RegistryID:              uuid.NullUUID{UUID: registryID, Valid: true},
		Name:                    "Resolver External MCP",
		Slug:                    slug,
		RegistryServerSpecifier: "resolver-extmcp",
	})
	require.NoError(t, err)

	toolURNString := "tools:externalmcp:" + slug + ":proxy"
	_, err = externalmcpRepo.CreateExternalMCPToolDefinition(ctx, externalmcp_repo.CreateExternalMCPToolDefinitionParams{
		ExternalMcpAttachmentID:    attachment.ID,
		ToolUrn:                    toolURNString,
		Type:                       "proxy",
		RemoteUrl:                  upstreamURL,
		TransportType:              externalmcp_types.TransportTypeStreamableHTTP,
		RequiresOauth:              true,
		OauthVersion:               "2.1",
		OauthAuthorizationEndpoint: conv.ToPGText(upstreamURL + "/authorize"),
		OauthTokenEndpoint:         conv.ToPGText(upstreamURL + "/token"),
		OauthRegistrationEndpoint:  pgtype.Text{},
		OauthScopesSupported:       []string{},
	})
	require.NoError(t, err)

	toolURN, err := urn.ParseTool(toolURNString)
	require.NoError(t, err)
	_, err = toolsetsRepo.CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{Valid: false},
	})
	require.NoError(t, err)

	remoteRepo := remotesessions_repo.New(ti.conn)
	remoteIssuer, err := remoteRepo.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         toolset.ProjectID,
		Slug:                              "resolver-extmcp-rsi-" + suffix,
		Issuer:                            "https://upstream.example/" + suffix,
		AuthorizationEndpoint:             conv.ToPGText("https://upstream.example/" + suffix + "/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://upstream.example/" + suffix + "/token"),
		RegistrationEndpoint:              pgtype.Text{},
		JwksUri:                           pgtype.Text{},
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)
	remoteClient, err := remoteRepo.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             toolset.ProjectID,
		RemoteSessionIssuerID: remoteIssuer.ID,
		UserSessionIssuerID:   userIssuer.ID,
		ClientID:              "resolver-extmcp-client-" + suffix,
		ClientSecretEncrypted: pgtype.Text{},
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	return issuerGatedExternalMCPFixture{
		Toolset:             toolset,
		UserSessionIssuer:   userIssuer,
		RemoteSessionClient: remoteClient,
		ToolName:            slug + "--ping",
	}
}
