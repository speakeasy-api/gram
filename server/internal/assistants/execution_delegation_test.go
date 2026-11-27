package assistants

import (
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestTurnDelegationExchangesOnlyForItsActorAndKeepsTheUser(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "execution_delegation")
	require.NoError(t, err)
	ctx := t.Context()
	project := newProvisioningProject(t, db, "execution-delegation")
	seedProjectRead(t, db, "user-1", project)
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(ctx, "org-test", project, "user-1", "Delegation", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil, nil)
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "execution-delegation", "execution-delegation", eventStatusPending)
	redisClient, err := assistantsInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential, AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	manager := assistanttokens.New("test-secret", db, engine, newTestPrincipalCredentials(t, db), cache.NewRedisCacheAdapter(redisClient))
	core.assistantTokens = manager
	var dispatched atomic.Pointer[string]
	core.runtime = testRuntimeBackend{backend: runtimeBackendGKE, runTurnToken: &dispatched}

	turn := func(source, payload string) string {
		t.Helper()
		_, err := core.processEventTurn(ctx, assistantThreadRecord{ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: source}, assistant, assistantRuntimeRecord{}, assistantThreadEventRecord{ID: uuid.New(), EventID: uuid.NewString(), NormalizedPayloadJSON: []byte(payload)})
		require.NoError(t, err)
		return *dispatched.Load()
	}
	actor := urn.NewWorkloadPrincipal(uuid.New(), "kit-runtime")

	delegation, err := manager.DelegateTurnCredential(ctx, turn(sourceKindDashboard, `{"text":"hi","user_id":"user-1"}`), actor)
	require.NoError(t, err)
	require.NotEmpty(t, delegation)
	require.False(t, principalcredential.IsToken(delegation), "the runtime never holds a credential that authenticates on its own")

	_, err = manager.ExchangeDelegation(ctx, delegation, urn.NewWorkloadPrincipal(uuid.New(), "other-runtime"))
	require.ErrorIs(t, err, principalcredential.ErrInvalid, "only the named actor may exchange a delegation")

	exchanged, err := manager.ExchangeDelegation(ctx, delegation, actor)
	require.NoError(t, err)
	authed, claims, err := manager.AuthorizeRuntime(ctx, "Bearer "+exchanged)
	require.NoError(t, err)
	require.Equal(t, "user-1", claims.UserID)
	require.Equal(t, thread.String(), claims.ThreadID)
	credential, ok := principalcredential.FromContext(authed)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeAgent, credential.Credential.Principal.Type)
	require.Equal(t, "user-1", credential.Credential.AuthorizerUserID)

	unattended, err := manager.DelegateTurnCredential(ctx, turn(sourceKindCron, `{}`), actor)
	require.NoError(t, err)
	require.Empty(t, unattended, "a turn with no person behind it delegates nothing")

	legacy, err := core.MintThreadScopedRuntimeToken(assistant, thread, "user-1")
	require.NoError(t, err)
	scoped, err := manager.DelegateTurnCredential(ctx, legacy, actor)
	require.NoError(t, err, "a thread-scoped runtime token is not a principal credential")
	require.Empty(t, scoped, "a turn without a principal credential delegates nothing")
}
