package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestEvaluateProjectGatewayForTurnCredential_HumanAndWorkloadEndToEnd(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	ctx := f.ctx
	ti := f.ti
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)
	grantProjectRead(t, ti, authCtx)

	// Stand up a real mock upstream MCP server representing an attached remote
	// server so we can list and execute its tool through the Project Gateway.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		var rpcReq struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&rpcReq); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch rpcReq.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"result": map[string]any{
					"protocolVersion": "2025-03-26",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "attached-mock", "version": "1.0.0"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "ping_attached",
						"description": "Ping the attached server",
						"inputSchema": map[string]any{"type": "object"},
					}},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      rpcReq.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "pong-from-attached"}},
					"isError": false,
				},
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(upstream.Close)

	fixtures := authclientfixtures.New(ti.conn)
	remoteID, serverID := uuid.New(), uuid.New()
	attachedSlug := "attached-" + serverID.String()[:6]
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, authclientfixtures.SeedRemoteMCPServerFixtureParams{
		ID: remoteID, ProjectID: *authCtx.ProjectID, Url: upstream.URL,
	}))
	require.NoError(t, fixtures.SeedMCPServerFixture(ctx, authclientfixtures.SeedMCPServerFixtureParams{
		ID:                serverID,
		ProjectID:         *authCtx.ProjectID,
		Slug:              pgtype.Text{String: attachedSlug, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true},
	}))

	assistantsID := platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug)
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, serverID.String())
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, assistantsID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, agentPrincipal, serverID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, agentPrincipal, assistantsID)

	profileVersionID := uuid.New()
	rows, err := upstreamclientsrepo.New(ti.conn).RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
		CredentialKind: upstreamclients.CredentialKindExecutionProfileVersion,
		CredentialID:   profileVersionID,
		Source:         "assistant_profile",
		OrganizationID: authCtx.ActiveOrganizationID,
		AuthClientID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    serverID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	binding := seedAssistantThreadBinding(t, ctx, ti, authCtx)
	httpsServerURL, err := url.Parse("https://api.example.test")
	require.NoError(t, err)

	humanToken, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 agentPrincipal,
		AuthorizerUserID:          authCtx.UserID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: profileVersionID, Valid: true},
	}, binding)
	require.NoError(t, err)

	workloadToken, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewWorkloadPrincipal(f.issuerID, f.subject),
		AuthorizerUserID:          "",
		SelectionProfileVersionID: uuid.NullUUID{UUID: profileVersionID, Valid: true},
	}, binding)
	require.NoError(t, err)

	// Build a standalone worker-style ProjectGatewayEvaluator to prove both
	// *mcp.Service and *mcp.ProjectGatewayEvaluator return identical results.
	workerEvaluator := mcp.NewProjectGatewayEvaluator(mcp.ProjectGatewayEvaluatorConfig{
		Logger:               ti.logger,
		MeterProvider:        testenv.NewMeterProvider(t),
		DB:                   ti.conn,
		Environment:          environments.NewEnvironmentEntries(ti.logger, ti.conn, ti.enc, mcpmetadata_repo.New(ti.conn)),
		Encryption:           ti.enc,
		Cache:                testenv.NewMemoryCache(),
		Authz:                ti.authzEngine,
		Features:             ti.features,
		PrincipalCredentials: ti.principalCredentials,
		AssistantTokens:      ti.assistantTokens,
		PlatformExtras:       nil,
	})

	verifyCredential := func(label, token string) {
		eval, err := ti.service.EvaluateProjectGatewayForTurnCredential(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, token, httpsServerURL)
		require.NoError(t, err, label)
		require.True(t, eval.Admitted, "%s: reason=%s", label, eval.Reason)
		require.Equal(t, orggateway.ProjectURL(httpsServerURL, fixture.organizationSlug, fixture.projectSlug), eval.GatewayURL, label)

		workerEval, err := workerEvaluator.EvaluateProjectGatewayForTurnCredential(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, token, httpsServerURL)
		require.NoError(t, err, label)
		require.Equal(t, eval, workerEval, "%s: worker evaluator must agree with mcp.Service", label)

		seenServers := map[uuid.UUID]bool{}
		seenPlatform := map[string]bool{}
		for _, m := range eval.Members {
			if m.PlatformToolsetSlug != "" {
				seenPlatform[m.PlatformToolsetSlug] = true
			} else {
				seenServers[m.ServerID] = true
			}
		}
		// seedOrganizationGateway also seeds one public server in the project,
		// so the gateway admits both serverID and that public server.
		require.True(t, seenServers[serverID], "%s: attached server must be admitted", label)
		require.Len(t, seenServers, 2, label)
		require.Equal(t, map[string]bool{platformtools.AssistantsPlatformToolsetSlug: true}, seenPlatform, label)

		// Exercise the real Project Gateway HTTP handler with this turn credential:
		// 1) list_servers returns both the attached server and the assistants built-in.
		listed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
		require.NoError(t, err, label)
		require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
		require.Contains(t, listed.Body.String(), `"slug":"`+attachedSlug+`"`, label)
		require.Contains(t, listed.Body.String(), `"slug":"assistants"`, label)

		// 2) execute_tool on the attached server works through the gateway.
		attachedResp, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
			"name":      metamcp.ToolExecuteTool,
			"arguments": map[string]any{"name": attachedSlug + "--ping_attached", "arguments": map[string]any{}},
		}))
		require.NoError(t, err, label)
		require.Equal(t, http.StatusOK, attachedResp.Code, attachedResp.Body.String())
		attachedText, attachedErr := metaToolResultText(t, decodeRPCResponse(t, attachedResp))
		require.False(t, attachedErr, "%s: %s", label, attachedText)
		require.Contains(t, attachedText, "pong-from-attached", label)

		// 3) execute_read_only_tool on the built-in assistants tool works through the gateway.
		builtinResp, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
			"name": metamcp.ToolExecuteReadOnlyTool,
			"arguments": map[string]any{
				"name":      platformtools.AssistantsPlatformToolsetSlug + "--" + platformtools.ToolNameSkillsLoad,
				"arguments": map[string]any{"name": "nonexistent-skill"},
			},
		}))
		require.NoError(t, err, label)
		require.Equal(t, http.StatusOK, builtinResp.Code, builtinResp.Body.String())
		// Tool reached the built-in handler (not "unknown server").
		require.NotContains(t, builtinResp.Body.String(), "unknown server", label)
	}

	verifyCredential("human_turn_credential", humanToken)
	verifyCredential("workload_turn_credential", workloadToken)

	// Non-HTTPS URL must be rejected.
	httpServerURL, err := url.Parse("http://api.example.test")
	require.NoError(t, err)
	httpEval, err := ti.service.EvaluateProjectGatewayForTurnCredential(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, humanToken, httpServerURL)
	require.NoError(t, err)
	require.False(t, httpEval.Admitted)
	require.Equal(t, "non_https_gateway_url", httpEval.Reason)

	// Disabling project-gateway-machine-access immediately flips evaluation to ineligible.
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, false)
	flagOffEval, err := ti.service.EvaluateProjectGatewayForTurnCredential(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, humanToken, httpsServerURL)
	require.NoError(t, err)
	require.False(t, flagOffEval.Admitted)
	require.Equal(t, "machine_access_disabled", flagOffEval.Reason)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	// Turning off the org gateway immediately flips evaluation to ineligible.
	_, err = featuresrepo.New(ti.conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureOrganizationGatewayDisabled),
	})
	require.NoError(t, err)
	disabledEval, err := ti.service.EvaluateProjectGatewayForTurnCredential(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, humanToken, httpsServerURL)
	require.NoError(t, err)
	require.False(t, disabledEval.Admitted)
	require.Equal(t, string(orggateway.UnavailabilityTurnedOff), disabledEval.Reason)
}

func TestEvaluateProjectGatewayForTurnCredential_UnbackfilledAgentMissingBuiltin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_ = seedOrganizationGateway(t, ctx, ti)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)
	grantProjectRead(t, ti, authCtx)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Unbackfilled agent " + uuid.NewString()[:8],
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

	httpsServerURL, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	eval, err := ti.service.EvaluateProjectGatewayForTurnCredential(context.Background(), authCtx.ActiveOrganizationID, *authCtx.ProjectID, token, httpsServerURL)
	require.NoError(t, err)
	require.True(t, eval.Admitted)
	// No built-in grant -> members does NOT include "assistants", so assistant
	// set equality rejects gateway mode and falls back to per-server.
	for _, m := range eval.Members {
		require.NotEqual(t, platformtools.AssistantsPlatformToolsetSlug, m.PlatformToolsetSlug)
	}
}
