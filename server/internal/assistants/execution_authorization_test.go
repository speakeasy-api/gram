package assistants

import (
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func deleteAgentScope(t *testing.T, db *pgxpool.Pool, agentID string, scope authz.Scope) {
	t.Helper()
	agents := agentrepo.New(db)
	rows, err := agents.ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: uuid.MustParse(agentID)})
	require.NoError(t, err)
	deleted := 0
	for _, row := range rows {
		if row.Scope == string(scope) {
			_, err = agents.DeleteAgentPolicyGrant(t.Context(), agentrepo.DeleteAgentPolicyGrantParams{OrganizationID: "org-test", AgentID: uuid.MustParse(agentID), GrantID: row.ID})
			require.NoError(t, err)
			deleted++
		}
	}
	require.NotZero(t, deleted)
}

func TestAgentBackedTurnRunsAsItsWorkload(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_authorization")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "execution-authorization")
	seedProjectRead(t, db, "user-2", project)
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Execution", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	toolset, err := toolsetsrepo.New(db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{OrganizationID: "org-test", ProjectID: project, Name: "Business", Slug: "business", Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: pgtype.Text{}, McpEnabled: true})
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "execution-authorization", "execution-authorization", eventStatusPending)

	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	manager := assistanttokens.New("test-secret", db, engine, newTestExecutionIssuer(t), testIdentityService)
	core.assistantTokens = manager
	var dispatched atomic.Pointer[string]
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnToken: &dispatched}

	_, err = core.processEventTurn(t.Context(), assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: sourceKindDashboard}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: "dashboard-event", NormalizedPayloadJSON: []byte(`{"text":"hi","user_id":"user-2"}`)})
	require.NoError(t, err)
	token := *dispatched.Load()
	require.True(t, assistanttokens.IsExecutionToken(token))
	_, err = manager.Validate(token)
	require.Error(t, err, "execution tokens never pass as legacy runtime tokens")

	ctx, claims, err := manager.AuthorizeRuntime(t.Context(), "Bearer "+token)
	require.NoError(t, err)
	require.Equal(t, "user-2", claims.UserID)
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)
	invoker, ok := contextvalues.AssistantInvoker(ctx)
	require.True(t, ok)
	require.Equal(t, "user-2", invoker)

	_, err = manager.AuthorizeBusiness(t.Context(), token, toolset.ID)
	require.NoError(t, err, "the agent's starting access covers project MCP servers")
	_, err = manager.AuthorizeBusiness(t.Context(), token, uuid.New())
	require.Error(t, err)

	deleteAgentScope(t, db, *assistant.AgentID, authz.ScopeMCPConnect)
	deleteAgentScope(t, db, *assistant.AgentID, authz.ScopeMCPRead)
	_, err = manager.AuthorizeBusiness(t.Context(), token, toolset.ID)
	require.Error(t, err, "business access follows the agent's live policy")
	_, _, err = manager.AuthorizeRuntime(t.Context(), token)
	require.NoError(t, err, "running needs no business grant")
}

func TestUnmigratedAssistantDispatchKeepsOriginalToken(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_legacy_token")
	require.NoError(t, err)
	project, id, _, _ := insertAssistantFixture(t, db)
	thread := seedThreadWithEvent(t, db, id, "legacy-token", "legacy-token", eventStatusPending)
	core := newProvisioningCore(t, db)
	assistant, err := core.getAssistantForDispatch(t.Context(), id)
	require.NoError(t, err)
	manager := assistanttokens.New("legacy-test-secret", db, nil, nil, testIdentityService)
	core.assistantTokens = manager
	var dispatched atomic.Pointer[string]
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnToken: &dispatched}
	_, err = core.processEventTurn(t.Context(), assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: id, SourceKind: sourceKindCron}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: "legacy-token", NormalizedPayloadJSON: []byte(`{}`)})
	require.NoError(t, err)
	require.False(t, assistanttokens.IsExecutionToken(*dispatched.Load()))
	claims, err := manager.Validate(*dispatched.Load())
	require.NoError(t, err)
	require.Equal(t, assistant.CreatedByUserID, claims.UserID)
	require.Equal(t, assistantRuntimeTokenTTL, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
}
