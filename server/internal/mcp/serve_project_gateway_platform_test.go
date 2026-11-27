package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/admin"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServeProjectGateway_BuiltinAssistantsToolset(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Builtin gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	grantProjectRead(t, ti, authCtx)
	binding := seedAssistantThreadBinding(t, ctx, ti, authCtx)

	token, err := ti.assistantTokens.MintTurnCredential(ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		AuthorizerUserID:          authCtx.UserID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, binding)
	require.NoError(t, err)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	assistantsID := platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug)

	// Without mcp:connect on the built-in, the agent does not see or reach it.
	listed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.NotContains(t, listed.Body.String(), `"slug":"assistants"`)

	refusedDescribe, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeServer,
		"arguments": map[string]any{"server": platformtools.AssistantsPlatformToolsetSlug},
	}))
	require.NoError(t, err)
	require.Contains(t, refusedDescribe.Body.String(), "unknown server")

	refusedExec, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      platformtools.AssistantsPlatformToolsetSlug + "--" + platformtools.ToolNameSkillsLoad,
			"arguments": map[string]any{"name": "missing-skill"},
		},
	}))
	require.NoError(t, err)
	require.Contains(t, refusedExec.Body.String(), "unknown server")

	// Grant mcp:connect on PlatformToolsetID("assistants") to owner and agent.
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, assistantsID.String())
	agentGrantID := seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), assistantsID)

	// Built-in appears in list_servers and connection_status with no persisted row.
	listed, err = serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"slug":"assistants"`)
	require.Contains(t, listed.Body.String(), platformtools.PlatformToolsetName(platformtools.AssistantsPlatformToolsetSlug))

	statusResp, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolConnectionStatus,
		"arguments": map[string]any{},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, statusResp.Code, statusResp.Body.String())
	statusResult := decodeMetaToolResult(t, decodeRPCResponse(t, statusResp))
	require.False(t, statusResult.IsError)
	var statusPayload metamcp.ConnectionStatusResult
	require.NoError(t, json.Unmarshal(statusResult.StructuredContent, &statusPayload))
	var foundStatus bool
	for _, s := range statusPayload.Servers {
		if s.Slug == platformtools.AssistantsPlatformToolsetSlug {
			foundStatus = true
			require.Equal(t, metamcp.SignInNotRequired, s.Status)
		}
	}
	require.True(t, foundStatus, "connection_status must include admitted built-in toolset")

	// describe_server and describe_tools expose the built-in tool without
	// requiring thread_id because the turn credential binds the assistant thread.
	qualifiedSkillLoad := platformtools.AssistantsPlatformToolsetSlug + "--" + platformtools.ToolNameSkillsLoad
	described, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeServer,
		"arguments": map[string]any{"server": platformtools.AssistantsPlatformToolsetSlug},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, described.Code, described.Body.String())
	require.Contains(t, described.Body.String(), qualifiedSkillLoad)

	describedTools, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeTools,
		"arguments": map[string]any{"tools": []string{qualifiedSkillLoad}},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, describedTools.Code, describedTools.Body.String())
	require.Contains(t, describedTools.Body.String(), qualifiedSkillLoad)
	require.NotContains(t, describedTools.Body.String(), `"thread_id"`, "bound turn context must hide fallback thread_id")

	// Calling the thread-keyed built-in tool reaches the executor with the bound
	// assistant thread and returns its non-error result.
	executed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      qualifiedSkillLoad,
			"arguments": map[string]any{"name": "nonexistent-skill"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.False(t, isError, text)
	require.Contains(t, text, "no skills attached", "tool must resolve the bound thread and reach skill lookup: %s", text)

	// No toolsets or mcp_servers row exists for the synthetic built-in.
	_, err = toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID: assistantsID, ProjectID: *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID: assistantsID, ProjectID: *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// A human user on the Project Gateway never sees built-in toolsets.
	humanToken := mintProjectGatewayBearer(t, ti, fixture, fixture.projectSlug)
	humanListed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, humanToken, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, humanListed.Code, humanListed.Body.String())
	require.NotContains(t, humanListed.Body.String(), `"slug":"assistants"`)

	// Removing the agent's grant removes the built-in immediately.
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID, ID: agentGrantID,
	})
	require.NoError(t, err)
	afterRevoke, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.NotContains(t, afterRevoke.Body.String(), `"slug":"assistants"`)
}

func TestServeProjectGateway_BuiltinManagedOnlyPlatformToolset(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	grantProjectRead(t, ti, authCtx)
	grantLiveOrgAdmin(t, ti, authCtx)

	managedID, managedAgentID := managedAssistantWithAgent(t, ti, authCtx)
	regularID := createAssistant(t, ti, authCtx, "Regular assistant")
	regularAgent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Regular assistant agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	platformID := platformtools.PlatformToolsetID(platformtools.PlatformMCPReadToolsetSlug)
	slackID := platformtools.PlatformToolsetID(platformtools.SpeakeasySlackPlatformToolsetSlug)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, platformID.String())
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, slackID.String())

	// Grant both agents mcp:connect on platform and speakeasy-slack.
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, regularAgent.ID.String()), platformID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, regularAgent.ID.String()), slackID)
	managedPlatformGrant := seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, managedAgentID.String()), platformID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, managedAgentID.String()), slackID)

	regularToken := mintAgentTurnCredential(t, ti, authCtx, regularID, regularAgent.ID)
	managedToken := mintAgentTurnCredential(t, ti, authCtx, managedID, managedAgentID)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	// Non-managed assistant must not see platform or speakeasy-slack even with grants.
	regularListed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, regularToken, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, regularListed.Code, regularListed.Body.String())
	require.NotContains(t, regularListed.Body.String(), `"slug":"platform"`)
	require.NotContains(t, regularListed.Body.String(), `"slug":"speakeasy-slack"`)

	regularExec, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, regularToken, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      platformtools.PlatformMCPReadToolsetSlug + "--list_projects",
			"arguments": map[string]any{},
		},
	}))
	require.NoError(t, err)
	require.Contains(t, regularExec.Body.String(), "unknown server")

	// Managed assistant sees both platform and speakeasy-slack and can call platform tools.
	managedListed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, managedToken, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, managedListed.Code, managedListed.Body.String())
	require.Contains(t, managedListed.Body.String(), `"slug":"platform"`)
	require.Contains(t, managedListed.Body.String(), `"slug":"speakeasy-slack"`)

	described, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, managedToken, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      metamcp.ToolDescribeServer,
		"arguments": map[string]any{"server": platformtools.PlatformMCPReadToolsetSlug},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, described.Code, described.Body.String())
	require.Contains(t, described.Body.String(), "platform--list_projects")

	executed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, managedToken, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      platformtools.PlatformMCPReadToolsetSlug + "--list_projects",
			"arguments": map[string]any{},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.False(t, isError, text)
	require.Contains(t, text, authCtx.ProjectID.String())

	// Revoking mcp:connect hides platform even for the managed assistant.
	_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID, ID: managedPlatformGrant,
	})
	require.NoError(t, err)
	afterRevoke, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, managedToken, listServersBody(t))
	require.NoError(t, err)
	require.NotContains(t, afterRevoke.Body.String(), `"slug":"platform"`)
	require.Contains(t, afterRevoke.Body.String(), `"slug":"speakeasy-slack"`)

	// Verify no persisted row exists for platform or speakeasy-slack.
	for _, id := range []uuid.UUID{platformID, slackID} {
		_, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
			ID: id, ProjectID: *authCtx.ProjectID,
		})
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
}

func TestServeProjectGateway_BuiltinAssistantsToolset_WorkloadTurn(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, f.ctx, f.ti)
	assistantsID := platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug)
	binding := seedAssistantThreadBinding(t, f.ctx, f.ti, authCtx)

	token, err := f.ti.assistantTokens.MintTurnCredential(f.ctx, principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewWorkloadPrincipal(f.issuerID, f.subject),
		AuthorizerUserID:          "",
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, binding)
	require.NoError(t, err)
	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	qualifiedSkillLoad := platformtools.AssistantsPlatformToolsetSlug + "--" + platformtools.ToolNameSkillsLoad

	// Without mcp:connect on the built-in, the workload turn cannot list or call it.
	deniedListed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, deniedListed.Code, deniedListed.Body.String())
	require.NotContains(t, deniedListed.Body.String(), `"slug":"assistants"`)

	deniedExec, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      qualifiedSkillLoad,
			"arguments": map[string]any{"name": "nonexistent-skill"},
		},
	}))
	require.NoError(t, err)
	require.Contains(t, deniedExec.Body.String(), "unknown server")

	// After granting mcp:connect, the workload turn lists and executes the built-in tool.
	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, assistantsID.String())
	seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), assistantsID)

	listed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"slug":"assistants"`)

	executed, err := serveProjectGatewayHTTP(t, f.ti, fixture, fixture.projectSlug, token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      qualifiedSkillLoad,
			"arguments": map[string]any{"name": "nonexistent-skill"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.False(t, isError, text)
	require.Contains(t, text, "no skills attached", "workload turn must resolve bound thread and execute built-in tool: %s", text)
}

func TestServeProjectGateway_ProvisionedAssistantHasBuiltinsAndRevocationRemovesThem(t *testing.T) {
	t.Parallel()
	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := seedOrganizationGateway(t, ctx, ti)
	grantProjectRead(t, ti, authCtx)
	grantLiveOrgAdmin(t, ti, authCtx)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, authz.WildcardResource)
	ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, authCtx.ActiveOrganizationID, true)

	identities := assistantidentity.New("https://platform.example.invalid", audit.NewLogger())

	// Provision a brand-new ordinary assistant with no manual agent grant step.
	ordinaryID := createAssistant(t, ti, authCtx, "Provisioned ordinary")
	require.NoError(t, pgx.BeginFunc(ctx, ti.conn, func(tx pgx.Tx) error {
		return identities.Provision(ctx, tx, assistantidentity.ProvisionParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			AssistantID:    ordinaryID,
			ActorUserID:    authCtx.UserID,
			Staff:          nil,
		})
	}))
	ordinaryStates, err := assistantidentity.States(ctx, ti.conn, *authCtx.ProjectID, []uuid.UUID{ordinaryID})
	require.NoError(t, err)
	require.NotNil(t, ordinaryStates[ordinaryID].AgentID)
	ordinaryAgentID := *ordinaryStates[ordinaryID].AgentID

	// Provision a brand-new managed assistant with no manual agent grant step.
	managedID := createAssistant(t, ti, authCtx, "Provisioned managed")
	require.NoError(t, assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(ctx, assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: managedID,
	}))
	require.NoError(t, pgx.BeginFunc(ctx, ti.conn, func(tx pgx.Tx) error {
		return identities.Provision(ctx, tx, assistantidentity.ProvisionParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			AssistantID:    managedID,
			ActorUserID:    authCtx.UserID,
			Staff:          nil,
		})
	}))
	managedStates, err := assistantidentity.States(ctx, ti.conn, *authCtx.ProjectID, []uuid.UUID{managedID})
	require.NoError(t, err)
	require.NotNil(t, managedStates[managedID].AgentID)
	managedAgentID := *managedStates[managedID].AgentID

	ordinaryToken := mintAgentTurnCredential(t, ti, authCtx, ordinaryID, ordinaryAgentID)
	managedToken := mintAgentTurnCredential(t, ti, authCtx, managedID, managedAgentID)

	// New ordinary assistant sees and can call assistants built-in immediately.
	ordinaryListed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, ordinaryToken, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, ordinaryListed.Code, ordinaryListed.Body.String())
	require.Contains(t, ordinaryListed.Body.String(), `"slug":"assistants"`)
	require.NotContains(t, ordinaryListed.Body.String(), `"slug":"platform"`)
	require.NotContains(t, ordinaryListed.Body.String(), `"slug":"speakeasy-slack"`)

	qualifiedSkillLoad := platformtools.AssistantsPlatformToolsetSlug + "--" + platformtools.ToolNameSkillsLoad
	executed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, ordinaryToken, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": metamcp.ToolExecuteTool,
		"arguments": map[string]any{
			"name":      qualifiedSkillLoad,
			"arguments": map[string]any{"name": "nonexistent-skill"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, executed.Code, executed.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, executed))
	require.False(t, isError, text)
	require.Contains(t, text, "no skills attached")

	// New managed assistant sees all three built-in toolsets with no manual step.
	managedListed, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, managedToken, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, managedListed.Code, managedListed.Body.String())
	require.Contains(t, managedListed.Body.String(), `"slug":"assistants"`)
	require.Contains(t, managedListed.Body.String(), `"slug":"platform"`)
	require.Contains(t, managedListed.Body.String(), `"slug":"speakeasy-slack"`)

	// Remove the project-wide mcp:connect * grant from the ordinary agent: its
	// explicit built-in grant still keeps the assistants toolset available.
	agents := agentsrepo.New(ti.conn)
	rows, err := agents.ListAgentPolicyGrants(ctx, agentsrepo.ListAgentPolicyGrantsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		AgentID:        ordinaryAgentID,
	})
	require.NoError(t, err)
	assistantsID := platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug).String()
	var explicitBuiltinGrantID uuid.UUID
	for _, row := range rows {
		if row.Scope != string(authz.ScopeMCPConnect) {
			continue
		}
		sel, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		switch sel[authz.SelectorKeyResourceID] {
		case authz.WildcardResource:
			_, err = agents.DeleteAgentPolicyGrant(ctx, agentsrepo.DeleteAgentPolicyGrantParams{
				OrganizationID: authCtx.ActiveOrganizationID,
				AgentID:        ordinaryAgentID,
				GrantID:        row.ID,
			})
			require.NoError(t, err)
		case assistantsID:
			explicitBuiltinGrantID = row.ID
		}
	}
	require.NotEqual(t, uuid.Nil, explicitBuiltinGrantID, "provisioning must write an explicit built-in grant row")

	afterWildcardRemoved, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, ordinaryToken, listServersBody(t))
	require.NoError(t, err)
	require.Contains(t, afterWildcardRemoved.Body.String(), `"slug":"assistants"`)

	// Removing the explicit built-in grant removes the built-in from the gateway.
	_, err = agents.DeleteAgentPolicyGrant(ctx, agentsrepo.DeleteAgentPolicyGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		AgentID:        ordinaryAgentID,
		GrantID:        explicitBuiltinGrantID,
	})
	require.NoError(t, err)
	afterBuiltinRemoved, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, ordinaryToken, listServersBody(t))
	require.NoError(t, err)
	require.NotContains(t, afterBuiltinRemoved.Body.String(), `"slug":"assistants"`)

	// Running the per-org backfill restores the built-in through the gateway.
	staffCtx := contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "staff-session",
		OIDCSubject: "staff-subject",
		Email:       "staff@example.invalid",
		Name:        "Staff Member",
		HD:          "speakeasy.com",
	})
	require.NoError(t, pgx.BeginFunc(staffCtx, ti.conn, func(tx pgx.Tx) error {
		run, err := admin.BackfillAssistantBuiltinGrantsTx(staffCtx, tx, testenv.NewLogger(t), audit.NewLogger(), authCtx.ActiveOrganizationID)
		require.NoError(t, err)
		require.Equal(t, int64(1), run.Granted)
		require.Equal(t, int64(1), run.AlreadyGranted)
		return nil
	}))
	afterBackfill, err := serveProjectGatewayHTTP(t, ti, fixture, fixture.projectSlug, ordinaryToken, listServersBody(t))
	require.NoError(t, err)
	require.Contains(t, afterBackfill.Body.String(), `"slug":"assistants"`)
}
