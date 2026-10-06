package assistants

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
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

func newTestPrincipalCredentials(t *testing.T, db *pgxpool.Pool) *principalcredential.Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	signer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://platform.example.invalid", false)
	require.NoError(t, err)
	return principalcredential.New(signer, db)
}

func seedMCPConnect(t *testing.T, db *pgxpool.Pool, user string, project uuid.UUID) {
	t.Helper()
	selector := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource)
	selector[authz.SelectorKeyProjectID] = project.String()
	raw, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{OrganizationID: "org-test", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, user), Scope: string(authz.ScopeMCPConnect), Selectors: raw})
	require.NoError(t, err)
}

func TestAgentBackedTurnsRunWithPrincipalCredentials(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_authorization")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "execution-authorization")
	seedProjectRead(t, db, "user-1", project)
	seedProjectRead(t, db, "user-2", project)
	seedMCPConnect(t, db, "user-1", project)
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Execution", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true)
	require.NoError(t, err)
	toolset, err := toolsetsrepo.New(db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{OrganizationID: "org-test", ProjectID: project, Name: "Business", Slug: "business", Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: pgtype.Text{}, McpEnabled: true})
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "execution-authorization", "execution-authorization", eventStatusPending)

	redisClient, err := assistantsInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential, AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	manager := assistanttokens.New("test-secret", db, engine, newTestPrincipalCredentials(t, db), cache.NewRedisCacheAdapter(redisClient))
	core.assistantTokens = manager
	var dispatched atomic.Pointer[string]
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnToken: &dispatched}
	connect := authz.MCPCheck(authz.ScopeMCPConnect, toolset.ID.String(), project.String())

	turn := func(source, payload string) string {
		t.Helper()
		_, err := core.processEventTurn(t.Context(), assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: source}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: uuid.NewString(), NormalizedPayloadJSON: []byte(payload)})
		require.NoError(t, err)
		token := *dispatched.Load()
		require.True(t, assistanttokens.IsRuntimeCredential("Bearer "+token))
		_, err = manager.Validate(token)
		require.Error(t, err, "principal credentials never pass as legacy runtime tokens")
		return token
	}

	narrowed := turn(sourceKindDashboard, `{"text":"hi","user_id":"user-2"}`)
	ctx, _, err := manager.AuthorizeRuntime(t.Context(), narrowed)
	require.NoError(t, err)
	admitted, err := engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.Error(t, engine.Require(admitted, connect), "a credential never exceeds what its authorizer could delegate at mint")

	seedMCPConnect(t, db, "user-2", project)
	human := turn(sourceKindDashboard, `{"text":"hi","user_id":"user-2"}`)
	ctx, claims, err := manager.AuthorizeRuntime(t.Context(), "Bearer "+human)
	require.NoError(t, err)
	require.Equal(t, "user-2", claims.UserID)
	require.Equal(t, thread.String(), claims.ThreadID)
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	require.True(t, ok)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, *assistant.AgentID), actor)
	credential, ok := contextvalues.PrincipalCredentialAuthorization(ctx)
	require.True(t, ok)
	require.Equal(t, "user-2", credential.AuthorizerUserID)
	admitted, err = engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, engine.Require(admitted, connect), "the agent's starting access covers project MCP servers")

	autonomous := turn(sourceKindCron, `{}`)
	ctx, claims, err = manager.AuthorizeRuntime(t.Context(), autonomous)
	require.NoError(t, err)
	require.Empty(t, claims.UserID, "a turn without a known human records none")
	actor, ok = contextvalues.AuthenticatedActor(ctx)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)
	admitted, err = engine.PrepareContext(ctx)
	require.NoError(t, err)
	require.NoError(t, engine.Require(admitted, connect))

	deleteAgentScope(t, db, *assistant.AgentID, authz.ScopeMCPConnect)
	deleteAgentScope(t, db, *assistant.AgentID, authz.ScopeMCPRead)
	for _, token := range []string{human, autonomous} {
		ctx, _, err := manager.AuthorizeRuntime(t.Context(), token)
		require.NoError(t, err, "running needs no business grant")
		admitted, err := engine.PrepareContext(ctx)
		require.NoError(t, err)
		require.Error(t, engine.Require(admitted, connect), "business access follows the agent's live policy")
	}
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
	manager := assistanttokens.New("legacy-test-secret", db, nil, nil, nil)
	core.assistantTokens = manager
	var dispatched atomic.Pointer[string]
	core.runtime = testRuntimeBackend{backend: runtimeBackendFlyIO, runTurnToken: &dispatched}
	_, err = core.processEventTurn(t.Context(), assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: id, SourceKind: sourceKindCron}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: "legacy-token", NormalizedPayloadJSON: []byte(`{}`)})
	require.NoError(t, err)
	require.False(t, assistanttokens.IsRuntimeCredential(*dispatched.Load()))
	claims, err := manager.Validate(*dispatched.Load())
	require.NoError(t, err)
	require.Equal(t, assistant.CreatedByUserID, claims.UserID)
	require.Equal(t, assistantRuntimeTokenTTL, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
}
