// remotesession_resolver_integration_test.go drives an end-to-end
// tools/call against an issuer-gated toolset holding an OAuth-secured HTTP
// tool. It pins the load-bearing claim of the resolver contract: the
// upstream request receives `Authorization: Bearer <stored token>`
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServePublic_UserSessionIssuerRemoteSessionResolvedTokenReachesHTTPTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	var capturedAuth atomic.Value
	recorder := newAuthorizationRecorder(t, &capturedAuth)

	fixture := createIssuerGatedHTTPToolFixture(t, ctx, ti, authCtx, "resolver-http", recorder.URL)

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

// newAuthorizationRecorder stands up an upstream API that stores the
// Authorization header of the requests it serves.
func newAuthorizationRecorder(t *testing.T, captured *atomic.Value) *httptest.Server {
	t.Helper()

	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			captured.Store(auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pong":true}`))
	}))
	t.Cleanup(recorder.Close)

	return recorder
}

type issuerGatedHTTPToolFixture struct {
	remoteSessionResolverFixture
	ToolName string
}

// createIssuerGatedHTTPToolFixture extends the remote-session resolver fixture
// with a new deployment whose only tool is an HTTP tool at upstreamURL secured
// by OAuth, so the resolved remote session token is what authenticates the
// upstream request.
func createIssuerGatedHTTPToolFixture(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
	slugPrefix string,
	upstreamURL string,
) issuerGatedHTTPToolFixture {
	t.Helper()

	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, slugPrefix)
	toolset := fixture.Toolset

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

	const toolName = "ping"
	toolURN := urn.NewTool(urn.ToolKindHTTP, "resolver-upstream", uuid.NewString()[:8])
	_, err = deployments_repo.New(ti.conn).CreateOpenAPIv3ToolDefinition(ctx, deployments_repo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           toolset.ProjectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{},
		ToolUrn:             toolURN,
		Name:                toolName,
		Summary:             "Ping the upstream API",
		Description:         "Ping the upstream API",
		Tags:                []string{},
		Security:            []byte(`[{"test_oauth": []}]`),
		HttpMethod:          http.MethodGet,
		Path:                "/ping",
		SchemaVersion:       "3.0.0",
		Schema:              []byte(`{}`),
		HeaderSettings:      []byte(`{}`),
		QuerySettings:       []byte(`{}`),
		PathSettings:        []byte(`{}`),
		ServerEnvVar:        "TEST_SERVER_URL",
		DefaultServerUrl:    conv.ToPGText(upstreamURL),
	})
	require.NoError(t, err)

	_, err = deployments_repo.New(ti.conn).CreateHTTPSecurity(ctx, deployments_repo.CreateHTTPSecurityParams{
		Key:                 "test_oauth",
		DeploymentID:        deploymentID,
		ProjectID:           uuid.NullUUID{UUID: toolset.ProjectID, Valid: true},
		Openapiv3DocumentID: uuid.NullUUID{},
		Type:                pgtype.Text{String: "oauth2", Valid: true},
		Name:                pgtype.Text{},
		InPlacement:         pgtype.Text{},
		Scheme:              pgtype.Text{},
		BearerFormat:        pgtype.Text{},
		EnvVariables:        []string{"TEST_OAUTH_ACCESS_TOKEN"},
		OauthTypes:          []string{"authorization_code"},
		OauthFlows:          []byte(`{"authorizationCode": {"scopes": {}}}`),
	})
	require.NoError(t, err)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	latest, err := toolsetsRepo.GetLatestToolsetVersion(ctx, toolset.ID)
	require.NoError(t, err)
	_, err = toolsetsRepo.CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       latest.Version + 1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{UUID: latest.ID, Valid: true},
	})
	require.NoError(t, err)

	return issuerGatedHTTPToolFixture{remoteSessionResolverFixture: fixture, ToolName: toolName}
}
