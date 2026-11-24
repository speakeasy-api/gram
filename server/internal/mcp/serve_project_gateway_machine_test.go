package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServeProjectGateway_WorkloadMachineAccess(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)
	fixture := seedOrganizationGateway(t, f.ctx, f.ti)
	grantedID := mcpServerBySlug(t, f.ctx, f.ti, *authCtx.ProjectID, fixture.granted)
	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedID.String())
	seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), grantedID)
	root := strings.TrimSuffix(f.ti.serverURL.String(), "/") + "/mcp/gateway/" + fixture.organizationSlug + "/projects/" + fixture.projectSlug

	_, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), root)
	require.NoError(t, err)
	require.False(t, ok)
	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, f.fx.orgID, true)
	target, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), root)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, root+"/token", target.TokenEndpoint)
	require.Equal(t, root, target.Resource)

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {f.assertion(t, target.Audience)},
		oauthwire.ParamResource:  {target.Resource},
	}
	router := goahttp.NewMuxer()
	mcp.Attach(router, f.ti.service.Service, nil)
	request := httptest.NewRequest(http.MethodPost, target.TokenEndpoint, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	bearer := decodeSharedTokenResponse(t, response).AccessToken
	require.NotEmpty(t, bearer)

	listed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), fixture.open)
	require.Contains(t, listed.Body.String(), fixture.granted)
	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'meta_discovery'
		   AND tool_name = 'list_servers'
		   AND toString(attributes.gram.authorization.actor.type) = 'agent'
		   AND toString(attributes.gram.authorization.actor.id) = ?
		   AND toString(attributes.gram.workload_issuer.id) = ?
		   AND toString(attributes.gram.workload.subject) = ?`,
		1, authCtx.ProjectID.String(), f.agentID.String(), f.issuerID.String(), f.subject)

	_, err = serveOrganizationGatewayHTTP(t, f.ctx, f.ti, fixture.organizationSlug, bearer, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestServeProjectGateway_AgentMachineAccess(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	fixture := seedOrganizationGateway(t, ctx, ti)
	grantedID := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fixture.granted)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Project gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), grantedID)
	token := mintAgentKey(t, ctx, ti, agent.ID, grantedID)

	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	anonymousToken := mintProjectGatewayBearerForSubject(t, ti, fixture, fixture.projectSlug, urn.NewAnonymousSubject(uuid.NewString()))
	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, anonymousToken, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	assistantToken := mintAssistantBearerForOwner(t, ti, authCtx)
	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, assistantToken, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	listed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), fixture.granted)
	require.NotContains(t, listed.Body.String(), fixture.denied)
	require.Contains(t, listed.Body.String(), fixture.open)
	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'meta_discovery'
		   AND tool_name = 'list_servers'
		   AND toString(attributes.gram.authorization.actor.type) = 'agent'
		   AND toString(attributes.gram.authorization.actor.id) = ?`,
		1, authCtx.ProjectID.String(), agent.ID.String())

	_, err = serveOrganizationGatewayHTTP(t, ctx, ti, fixture.organizationSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func seedExecutableHostedProjectMember(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
	toolName string,
	responseBody string,
) (hostedMemberFixture, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(upstream.Close)

	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "turn-ts-"+uuid.NewString()[:8])
	deploymentID, err := deploymentsrepo.New(ti.conn).InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	require.NoError(t, deploymentsrepo.New(ti.conn).CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))
	toolURN := urn.NewTool(urn.ToolKindHTTP, toolName, uuid.NewString()[:8])
	_, err = deploymentsrepo.New(ti.conn).CreateOpenAPIv3ToolDefinition(ctx, deploymentsrepo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:        *authCtx.ProjectID,
		DeploymentID:     deploymentID,
		ToolUrn:          toolURN,
		Name:             toolName,
		Summary:          toolName + " summary",
		Description:      toolName + " description",
		Tags:             []string{},
		HttpMethod:       http.MethodGet,
		Path:             "/ping",
		SchemaVersion:    "3.0.0",
		Schema:           []byte(`{}`),
		ServerEnvVar:     "TEST_SERVER_URL",
		DefaultServerUrl: conv.ToPGText(upstream.URL),
		Security:         []byte(`[]`),
		HeaderSettings:   []byte(`{}`),
		QuerySettings:    []byte(`{}`),
		PathSettings:     []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID:    toolset.ID,
		Version:      1,
		ToolUrns:     []urn.Tool{toolURN},
		ResourceUrns: []urn.Resource{},
	})
	require.NoError(t, err)

	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	memberSlug := "turn-hosted-" + uuid.NewString()[:8]
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:         serverID,
		ProjectID:  *authCtx.ProjectID,
		Name:       conv.ToPGText("Turn hosted member"),
		Slug:       conv.ToPGText(memberSlug),
		ToolsetID:  uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	return hostedMemberFixture{
		slug:      memberSlug,
		serverID:  server.ID,
		toolsetID: toolset.ID,
		toolURNs:  map[string]urn.Tool{toolName: toolURN},
	}, upstream
}

func seedAssistantThreadBinding(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
) assistanttokens.RuntimeBinding {
	t.Helper()
	assistantID := createAssistant(t, ti, authCtx, "Gateway turn assistant")
	queries := assistantsrepo.New(ti.conn)
	chatID := uuid.New()
	require.NoError(t, queries.UpsertAssistantChat(ctx, assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         pgtype.Text{String: authCtx.UserID, Valid: true},
		Title:          pgtype.Text{},
	}))
	threadID, err := queries.UpsertAssistantThread(ctx, assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     *authCtx.ProjectID,
		CorrelationID: "gw-turn-" + uuid.NewString()[:8],
		ChatID:        chatID,
		SourceKind:    "dashboard",
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)
	return assistanttokens.RuntimeBinding{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		AssistantID:    assistantID,
		ThreadID:       threadID,
		ChatID:         chatID,
	}
}

func TestServeProjectGateway_AssistantTurnAgentCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	member, _ := seedExecutableHostedProjectMember(t, ctx, ti, authCtx, "echo_turn", `{"reply":"human turn ok"}`)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Human turn gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	grantProjectRead(t, ti, authCtx)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, member.toolsetID.String())
	agentGrantID := seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), member.toolsetID)
	binding := seedAssistantThreadBinding(t, ctx, ti, authCtx)

	token, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		AuthorizerUserID:          authCtx.UserID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, binding)
	require.NoError(t, err)

	// Fails closed when project-gateway-machine-access is off.
	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)

	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	// Proves thread and chat binding on the Project Gateway request context.
	gatewaySlug := fixture.organizationSlug + "/projects/" + fixture.projectSlug
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, testenv.NewLogger(t), gatewaySlug, mcproute.OrganizationGateway)
	require.NoError(t, err)
	admittedCtx, _, _, err := ti.service.ApplyIssuerGate(ctx, httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
	require.NoError(t, err)
	gotPrincipal, hasPrincipal := contextvalues.GetAssistantPrincipal(admittedCtx)
	require.True(t, hasPrincipal, "turn credential must bind the assistant thread on the gateway path")
	require.Equal(t, contextvalues.AssistantPrincipal{AssistantID: binding.AssistantID, ThreadID: binding.ThreadID}, gotPrincipal)
	gotChatID, hasChat := assistanttokens.RuntimeChatID(admittedCtx)
	require.True(t, hasChat, "turn credential must bind the thread chat on the gateway path")
	require.Equal(t, binding.ChatID, gotChatID)

	listed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), member.slug)
	require.Contains(t, listed.Body.String(), fixture.open)
	require.NotContains(t, listed.Body.String(), fixture.denied)

	described, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeServer,
		"arguments": map[string]any{"server": member.slug},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, described.Code, described.Body.String())
	require.Contains(t, described.Body.String(), member.slug+"--echo_turn")

	executed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      member.slug + "--echo_turn",
			"arguments": map[string]any{},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.False(t, isError, text)
	require.Contains(t, text, "human turn ok")

	// Rejected at organization-wide gateway and another project's gateway.
	_, err = serveOrganizationGatewayHTTP(t, ctx, ti, fixture.organizationSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	_, err = serveProjectGatewayHTTP(t, ti, fixture, otherProject.Slug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// Revoking the agent's grant immediately hides the member.
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID, ID: agentGrantID,
	})
	require.NoError(t, err)
	afterRevoke, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.NotContains(t, afterRevoke.Body.String(), member.slug)

	// Deleting the bound thread immediately rejects the turn credential.
	require.NoError(t, assistantsrepo.New(ti.conn).SoftDeleteAssistantThread(ctx, assistantsrepo.SoftDeleteAssistantThreadParams{
		ID: binding.ThreadID, ProjectID: *authCtx.ProjectID,
	}))
	_, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestServeProjectGateway_AssistantTurnWorkloadCredential(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, f.ctx, f.ti)
	member, _ := seedExecutableHostedProjectMember(t, f.ctx, f.ti, authCtx, "echo_autonomous", `{"reply":"autonomous turn ok"}`)
	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, member.toolsetID.String())
	agentGrantID := seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), member.toolsetID)
	binding := seedAssistantThreadBinding(t, f.ctx, f.ti, authCtx)

	token, err := f.ti.assistantTokens.MintTurnCredential(f.ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewWorkloadPrincipal(f.issuerID, f.subject),
		AuthorizerUserID:          "",
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, binding)
	require.NoError(t, err)

	// Fails closed when project-gateway-machine-access is off.
	_, err = serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)

	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	// Proves thread and chat binding on the Project Gateway request context.
	gatewaySlug := fixture.organizationSlug + "/projects/" + fixture.projectSlug
	endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(f.ctx, testenv.NewLogger(t), gatewaySlug, mcproute.OrganizationGateway)
	require.NoError(t, err)
	admittedCtx, _, _, err := f.ti.service.ApplyIssuerGate(f.ctx, httptest.NewRecorder(), token, f.ti.serverURL.String(), endpoint)
	require.NoError(t, err)
	gotPrincipal, hasPrincipal := contextvalues.GetAssistantPrincipal(admittedCtx)
	require.True(t, hasPrincipal, "workload turn credential must bind the assistant thread on the gateway path")
	require.Equal(t, contextvalues.AssistantPrincipal{AssistantID: binding.AssistantID, ThreadID: binding.ThreadID}, gotPrincipal)
	gotChatID, hasChat := assistanttokens.RuntimeChatID(admittedCtx)
	require.True(t, hasChat, "workload turn credential must bind the thread chat on the gateway path")
	require.Equal(t, binding.ChatID, gotChatID)

	listed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), member.slug)
	require.Contains(t, listed.Body.String(), fixture.open)
	require.NotContains(t, listed.Body.String(), fixture.denied)

	described, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeServer,
		"arguments": map[string]any{"server": member.slug},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, described.Code, described.Body.String())
	require.Contains(t, described.Body.String(), member.slug+"--echo_autonomous")

	executed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      member.slug + "--echo_autonomous",
			"arguments": map[string]any{},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.False(t, isError, text)
	require.Contains(t, text, "autonomous turn ok")

	// Rejected at organization-wide gateway and another project's gateway.
	_, err = serveOrganizationGatewayHTTP(t, f.ctx, f.ti, fixture.organizationSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
	otherProject, err := projectsrepo.New(f.ti.conn).CreateProject(f.ctx, projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	_, err = serveProjectGatewayHTTP(t, f.ti, fixture, otherProject.Slug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// Revoking the agent's grant immediately hides the member and refuses execution.
	_, err = accessrepo.New(f.ti.conn).DeletePrincipalGrant(f.ctx, accessrepo.DeletePrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID, ID: agentGrantID,
	})
	require.NoError(t, err)
	afterRevoke, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.NotContains(t, afterRevoke.Body.String(), member.slug)
	refusedExec, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      member.slug + "--echo_autonomous",
			"arguments": map[string]any{},
		},
	}))
	require.NoError(t, err)
	require.Contains(t, refusedExec.Body.String(), "unknown server")

	// Deleting the bound thread immediately rejects the turn credential.
	require.NoError(t, assistantsrepo.New(f.ti.conn).SoftDeleteAssistantThread(f.ctx, assistantsrepo.SoftDeleteAssistantThreadParams{
		ID: binding.ThreadID, ProjectID: *authCtx.ProjectID,
	}))
	_, err = serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestServeProjectGateway_KitWorkloadBuiltinsAndLiveGrantChanges(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	fixture := seedOrganizationGateway(t, f.ctx, f.ti)
	grantProjectRead(t, f.ti, authCtx)
	binding := seedAssistantThreadBinding(t, f.ctx, f.ti, authCtx)
	member, _ := seedExecutableHostedProjectMember(t, f.ctx, f.ti, authCtx, "echo_kit", `{"reply":"kit project tool ok"}`)

	// Seed the minimal grant set needed for this test: explicit mcp:connect on
	// the built-in assistants toolset (zero project servers granted yet), plus
	// assistant:write on the target assistant for both owner and agent so
	// thread-keyed built-in calls pass authorization. The Project Gateway
	// jwt-bearer exchange must succeed even with zero project servers granted.
	assistantsID := platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug)
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, assistantsID.String())
	seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, agentPrincipal, assistantsID)
	assistantSel, err := authz.NewSelector(authz.ScopeAssistantWrite, binding.AssistantID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.ti.conn).UpsertPrincipalGrant(f.ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		Scope:          string(authz.ScopeAssistantWrite),
		Selectors:      assistantSel,
	})
	require.NoError(t, err)
	_, err = accessrepo.New(f.ti.conn).UpsertPrincipalGrant(f.ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   agentPrincipal,
		Scope:          string(authz.ScopeAssistantWrite),
		Selectors:      assistantSel,
	})
	require.NoError(t, err)
	// Give owner access to the private project member so granting the agent
	// later takes effect immediately.
	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, member.toolsetID.String())

	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, f.fx.orgID, true)
	root := strings.TrimSuffix(f.ti.serverURL.String(), "/") + "/mcp/gateway/" + fixture.organizationSlug + "/projects/" + fixture.projectSlug
	target, ok, err := f.ti.service.WorkloadTokenTargetFor(t.Context(), root)
	require.NoError(t, err)
	require.True(t, ok)

	form := url.Values{
		oauthwire.ParamGrantType: {oauthwire.GrantTypeJWTBearer},
		oauthwire.ParamAssertion: {f.assertion(t, target.Audience)},
		oauthwire.ParamResource:  {target.Resource},
	}
	router := goahttp.NewMuxer()
	mcp.Attach(router, f.ti.service.Service, nil)
	request := httptest.NewRequest(http.MethodPost, target.TokenEndpoint, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	bearer := decodeSharedTokenResponse(t, response).AccessToken
	require.NotEmpty(t, bearer)

	// 1. Built-in assistants toolset is listed and callable through the single
	// Project Gateway endpoint; ungranted private project server is hidden.
	listed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"slug":"assistants"`)
	require.NotContains(t, listed.Body.String(), member.slug)

	qualifiedSkillLoad := platformtools.AssistantsPlatformToolsetSlug + "--" + platformtools.ToolNameSkillsLoad
	builtinExec, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name": qualifiedSkillLoad,
			"arguments": map[string]any{
				"name":      "nonexistent-skill",
				"thread_id": binding.ThreadID.String(),
			},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, builtinExec.Code, builtinExec.Body.String())
	builtinText, builtinErr := metaToolResultText(t, decodeRPCResponse(t, builtinExec))
	require.False(t, builtinErr, builtinText)
	require.Contains(t, builtinText, "no skills attached")

	// 2. Granting mcp:connect on the private project server takes effect
	// immediately using the SAME workload session (no runtime replacement).
	agentGrantID := seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, agentPrincipal, member.toolsetID)
	afterGrant, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, afterGrant.Code, afterGrant.Body.String())
	require.Contains(t, afterGrant.Body.String(), `"slug":"assistants"`)
	require.Contains(t, afterGrant.Body.String(), member.slug)

	projectExec, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      member.slug + "--echo_kit",
			"arguments": map[string]any{},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, projectExec.Code, projectExec.Body.String())
	projectText, projectErr := metaToolResultText(t, decodeRPCResponse(t, projectExec))
	require.False(t, projectErr, projectText)
	require.Contains(t, projectText, "kit project tool ok")

	// 3. Revoking the grant removes access immediately on the next call using
	// the SAME workload session (no runtime replacement).
	_, err = accessrepo.New(f.ti.conn).DeletePrincipalGrant(f.ctx, accessrepo.DeletePrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID, ID: agentGrantID,
	})
	require.NoError(t, err)
	afterRevoke, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, afterRevoke.Code, afterRevoke.Body.String())
	require.NotContains(t, afterRevoke.Body.String(), member.slug)
	require.Contains(t, afterRevoke.Body.String(), `"slug":"assistants"`)

	refusedExec, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      member.slug + "--echo_kit",
			"arguments": map[string]any{},
		},
	}))
	require.NoError(t, err)
	require.Contains(t, refusedExec.Body.String(), "unknown server")
}
