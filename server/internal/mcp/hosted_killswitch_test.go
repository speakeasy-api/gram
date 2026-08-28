package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServePublic_HostedToolsCallKillswitch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotEmpty(t, authCtx.UserID)

	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, "killswitch-"+uuid.NewString()[:8])
	endpointSlug := "killswitch-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, endpointSlug, "public", uuid.NullUUID{}, issuerID)
	userToken := mintIssuerBearerForEndpointSubject(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID, urn.NewUserSubject(authCtx.UserID))
	sessionHeaders := map[string]string{"Mcp-Session-Id": "same-session"}

	before, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeToolsCallBody("missing_tool"), userToken, sessionHeaders)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, before.Code)
	require.NotContains(t, before.Body.String(), "mcp_tool_calls_paused")

	note := "Tool calls paused for maintenance."
	insertHostedKillswitchPrescription(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, mcpServer.ID, note)

	initialize, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), userToken, sessionHeaders)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, initialize.Code)
	require.NotContains(t, initialize.Body.String(), "mcp_tool_calls_paused")

	anonymousToken := mintIssuerBearerForEndpoint(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID)
	unsupported, err := servePublicHTTP(t, context.Background(), ti, endpointSlug, makeToolsCallBody("missing_tool"), anonymousToken, sessionHeaders)
	require.NoError(t, err)
	require.NotContains(t, unsupported.Body.String(), note)
	require.NotContains(t, unsupported.Body.String(), "mcp_tool_calls_paused")

	attachMissingRemoteSession(t, ctx, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, issuerID)

	for _, toolName := range []string{"missing_tool", "search_tools", "describe_tools", "execute_tool"} {
		response, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeToolsCallBody(toolName), userToken, sessionHeaders)
		require.NoError(t, err, toolName)
		require.Equal(t, http.StatusOK, response.Code, toolName)
		require.JSONEq(t, `{"jsonrpc":"2.0","id":3,"error":{"code":-32003,"message":"Tool calls paused for maintenance.","data":{"code":"mcp_tool_calls_paused"}}}`, response.Body.String(), toolName)
	}

	_, err = ti.conn.Exec(ctx, "DROP TABLE killswitch_prescriptions CASCADE") //nolint:glint // notestingrawsql: deterministic DDL breakage in this test's isolated database forces an evaluator failure
	require.NoError(t, err)

	unavailable, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeToolsCallBody("missing_tool"), userToken, sessionHeaders)
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","id":3,"error":{"code":-32603,"message":"Internal error"}}`, unavailable.Body.String())
	require.NotContains(t, unavailable.Body.String(), note)
	require.NotContains(t, unavailable.Body.String(), "mcp_tool_calls_paused")
}

func attachMissingRemoteSession(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID) {
	t.Helper()

	repo := remotesessionsrepo.New(ti.conn)
	suffix := uuid.NewString()
	remoteIssuer, err := repo.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		OrganizationID:                    conv.ToPGTextEmpty(organizationID),
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		Slug:                              "killswitch-rsi-" + suffix,
		Issuer:                            "https://upstream.example/" + suffix,
		AuthorizationEndpoint:             conv.ToPGText("https://upstream.example/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://upstream.example/token"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
	require.NoError(t, err)
	remoteClient, err := repo.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:        conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID:              "killswitch-client-" + suffix,
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, repo.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: remoteClient.ID,
		UserSessionIssuerID:   userSessionIssuerID,
	}))
}

func insertHostedKillswitchPrescription(t *testing.T, ctx context.Context, ti *testInstance, organizationID, userID string, serverID uuid.UUID, externalNote string) {
	t.Helper()

	err := testrepo.New(ti.conn).InsertKillswitchPrescriptionFixture(ctx, testrepo.InsertKillswitchPrescriptionFixtureParams{
		PrescriptionID: uuid.New(),
		OrganizationID: organizationID,
		DefinitionKey:  string(mcptoolexecution.DefinitionKeyMCPToolExecution),
		PrincipalKind:  string(mcptoolexecution.PrincipalKindUser),
		PrincipalKey:   userID,
		ResourceKind:   string(mcptoolexecution.ResourceKindMCPServer),
		ResourceScope:  "selected",
		InternalNote:   "test context",
		ExternalNote:   externalNote,
		ResourceKeys:   []string{serverID.String()},
	})
	require.NoError(t, err)
}
