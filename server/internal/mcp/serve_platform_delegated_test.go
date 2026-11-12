package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistant_platform_mcp_adapter"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// managedAssistantWithAgent creates the project's managed assistant, puts the
// org on the Platform MCP variant, and creates an agent owned by the context's
// user. The agent holds no policy grants of its own.
func managedAssistantWithAgent(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext) (uuid.UUID, uuid.UUID) {
	t.Helper()

	managedID := createAssistant(t, ti, authCtx, "Managed")
	require.NoError(t, assistantsrepo.New(ti.conn).CreateProjectManagedAssistant(t.Context(), assistantsrepo.CreateProjectManagedAssistantParams{
		ProjectID:   *authCtx.ProjectID,
		AssistantID: managedID,
	}))
	ti.features.SetFlagVariant(feature.FlagAssistantPlatformMCP, authCtx.ActiveOrganizationID, feature.VariantAssistantToolsPlatformMCP)

	agent, err := agentrepo.New(ti.conn).CreateAgent(t.Context(), agentrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Name:           "Project assistant agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	return managedID, agent.ID
}

// mintAgentTurnCredential mints the credential an agent-backed dashboard turn
// runs with: the agent acting for the context's user, bound to a new thread.
func mintAgentTurnCredential(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext, assistantID, agentID uuid.UUID) string {
	t.Helper()

	queries := assistantsrepo.New(ti.conn)
	chatID := uuid.New()
	require.NoError(t, queries.UpsertAssistantChat(t.Context(), assistantsrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         pgtype.Text{String: authCtx.UserID, Valid: true},
		Title:          pgtype.Text{},
	}))
	threadID, err := queries.UpsertAssistantThread(t.Context(), assistantsrepo.UpsertAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     *authCtx.ProjectID,
		CorrelationID: "delegated-" + uuid.NewString()[:8],
		ChatID:        chatID,
		SourceKind:    "dashboard",
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)

	token, err := ti.assistantTokens.MintTurnCredential(t.Context(), principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()),
		AuthorizerUserID:          authCtx.UserID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, assistanttokens.RuntimeBinding{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		AssistantID:    assistantID,
		ThreadID:       threadID,
		ChatID:         chatID,
	})
	require.NoError(t, err)
	return token
}

func grantProjectRead(t *testing.T, ti *testInstance, authCtx *contextvalues.AuthContext) {
	t.Helper()

	require.NoError(t, authz.PatchPrincipalGrants(
		t.Context(), ti.conn, authCtx.ActiveOrganizationID,
		urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		[]*authz.RoleGrant{{Scope: string(authz.ScopeProjectRead), Selectors: []authz.Selector{authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String())}}},
		nil,
	))
}

// platformToolCallBody is a tools/call request for name with no arguments.
func platformToolCallBody(t *testing.T, name string) []byte {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": map[string]any{}},
	})
	require.NoError(t, err)
	return body
}

func TestServePlatformToolset_AgentTurnActsForItsDelegator(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, agentID := managedAssistantWithAgent(t, ti, authCtx)
	grantProjectRead(t, ti, authCtx)
	grantLiveOrgAdmin(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, managedID, agentID)

	countBefore, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)

	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, platformToolCallBody(t, "list_projects"), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.False(t, jsonToolCallFailed(w.Body.Bytes()), "an agent turn with an org-admin delegator must reach platform tools: %s", w.Body.String())
	require.Contains(t, w.Body.String(), authCtx.ProjectID.String(), "the delegator's own project:read decides the listing; the agent holds no grants")

	countAfter, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)
	require.Equal(t, countBefore+1, countAfter)
	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionAssistantToolCall)
	require.NoError(t, err)
	require.Equal(t, string(urn.PrincipalTypeAgent), record.ActorType, "the tool call stays attributed to the agent")
	require.Equal(t, agentID.String(), record.ActorID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String(), metadata["authorizer_urn"], "the audit entry names the person the agent acted for")
}

func TestServePlatformToolset_AgentTurnDeniedWithoutDelegatorAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, agentID := managedAssistantWithAgent(t, ti, authCtx)
	grantProjectRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, managedID, agentID)

	w, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, platformToolCallBody(t, "list_projects"), token)
	require.True(t, err != nil || w.Code >= http.StatusBadRequest || jsonToolCallFailed(w.Body.Bytes()), "a delegator without org:admin must be refused: %s", w.Body.String())
	require.NotContains(t, w.Body.String(), authCtx.ProjectID.String())
}

func TestAssistantPlatformTool_WorkloadTurnIsToldItNeedsAPerson(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	grantLiveOrgAdmin(t, ti, authCtx)

	raw, _, err := ti.principalCredentials.Mint(principalcredential.Credential{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Principal:                 urn.NewWorkloadPrincipal(uuid.New(), "assistant-trigger:"+uuid.NewString()),
		AuthorizerUserID:          "",
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	workloadCtx, err := ti.principalCredentials.Authenticate(t.Context(), raw)
	require.NoError(t, err)
	workloadCtx = contextvalues.SetAssistantPrincipal(workloadCtx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})

	authorizer := platformmcp.NewLiveOrgAdminAuthorizer(ti.conn, ti.authzEngine)
	descriptors := platformmcp.NewRuntimeWithLifecycle(
		ti.logger, nil, nil, authorizer, "", "test-cursor-key",
		platformmcp.NewPostgresReader(ti.logger, ti.conn).WithAuthorization(ti.authzEngine), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		platformmcp.CatalogDescriptor{},
	).AssistantTools()
	var listProjects *assistant_platform_mcp_adapter.Tool
	for _, tool := range assistant_platform_mcp_adapter.Tools(descriptors, authorizer) {
		if tool.Descriptor().Name == "list_projects" {
			listProjects = &tool
			break
		}
	}
	require.NotNil(t, listProjects)

	var out bytes.Buffer
	err = listProjects.Call(workloadCtx, toolconfig.ToolCallEnv{}, strings.NewReader(`{}`), &out)
	require.NoError(t, err, "a refusal is the tool's answer, not a failure the runner would retry")
	var refusal struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &refusal))
	require.Equal(t, "person_required", refusal.Code)
}

// jsonRPCError reports whether the response is a protocol-level error, as a
// call to a tool the caller is not offered is, rather than the tool's answer.
func jsonRPCError(body []byte) bool {
	var response struct {
		Error json.RawMessage `json:"error"`
	}
	return json.Unmarshal(body, &response) != nil || len(response.Error) != 0
}

func TestServePlatformToolset_AgentTurnOffersLogReadToolsByDelegatorGrants(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, agentID := managedAssistantWithAgent(t, ti, authCtx)
	grantProjectRead(t, ti, authCtx)
	grantProjectLogRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, managedID, agentID)

	listed, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"query_mcp_traces"`, "the delegator's log:read decides what the turn is offered; the agent holds no grants")

	// Every platform tool call also needs the delegator to be an org admin.
	grantLiveOrgAdmin(t, ti, authCtx)
	called, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, platformToolCallBody(t, "query_mcp_traces"), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, called.Code, called.Body.String())
	require.False(t, jsonRPCError(called.Body.Bytes()), "an offered tool must be callable: %s", called.Body.String())
}

func TestServePlatformToolset_AgentTurnWithholdsLogReadToolsWithoutDelegatorLogRead(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, agentID := managedAssistantWithAgent(t, ti, authCtx)
	grantProjectRead(t, ti, authCtx)
	token := mintAgentTurnCredential(t, ti, authCtx, managedID, agentID)

	listed, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"list_projects"`)
	require.NotContains(t, listed.Body.String(), `"query_mcp_traces"`)

	called, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, platformToolCallBody(t, "query_mcp_traces"), token)
	require.True(t, err != nil || called.Code >= http.StatusBadRequest || jsonRPCError(called.Body.Bytes()), "a withheld tool must not be callable: %s", called.Body.String())
}

func TestServePlatformToolset_LegacyTurnOffersLogReadToolsByUserGrants(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithPlatformMCPReadTools(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	managedID, _ := managedAssistantWithAgent(t, ti, authCtx)
	token := mintAssistantToken(t, ti, authCtx, managedID)

	listed, err := servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.NotContains(t, listed.Body.String(), `"query_mcp_traces"`)

	grantProjectLogRead(t, ti, authCtx)
	listed, err = servePlatformHTTP(t, ti, platformtools.PlatformMCPReadToolsetSlug, toolsListBody(), token)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), `"query_mcp_traces"`)
}
