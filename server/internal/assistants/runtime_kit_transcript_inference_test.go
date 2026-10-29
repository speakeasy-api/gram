package assistants

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestKitInferenceLiveAdmissionAndCaptureOwnership(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "kit_inference_live")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "kit-inference-live")
	seedProjectRead(t, db, "user-1", project)
	seedMCPConnect(t, db, "user-1", project)
	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Inference", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	_, err = core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	resolution, err := core.identities.ResolveRuntime(t.Context(), db, "org-test", project, assistant.ID)
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "inference", "inference", eventStatusPending)
	queries := assistantrepo.New(db)
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: assistant.ID, RuntimeVersion: 3}))
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant.ID, AssistantThreadID: thread, Backend: "local", State: "active"}))
	runtimeRow, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: assistant.ID})
	require.NoError(t, err)
	runtime := runtimeRow.ID
	threadRow, err := assistantrepo.New(db).LoadAssistantThreadForBootstrap(t.Context(), assistantrepo.LoadAssistantThreadForBootstrapParams{ProjectID: project, ThreadID: thread})
	require.NoError(t, err)
	chatID := threadRow.ChatID
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential, AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	manager := assistanttokens.New("test-secret", db, engine, newTestPrincipalCredentials(t, db), nil)
	issuerID := uuid.New()
	subject := "runtime-generation-one"
	persist := func(subject string) {
		t.Helper()
		metadata, err := json.Marshal(map[string]any{"workload": map[string]any{"issuer_id": issuerID, "subject": subject, "identity": resolution}})
		require.NoError(t, err)
		require.NoError(t, queries.UpdateAssistantRuntimeMetadata(t.Context(), assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: project, RuntimeID: runtime, BackendMetadataJson: metadata}))
	}
	persist(subject)
	// The MCP authorizer separately verifies the bearer, live admission and
	// owner/agent inference policy. Attribution consumes only that trusted context.
	authorize := func(subject string) (context.Context, error) {
		ac := &contextvalues.AuthContext{ActiveOrganizationID: "org-test", ProjectID: &project}
		ctx := contextvalues.WithAuthenticatedActor(t.Context(), ac, urn.NewWorkloadPrincipal(issuerID, subject))
		ctx = contextvalues.WithWorkloadAgent(ctx, urn.NewPrincipal(urn.PrincipalTypeAgent, resolution.AgentID.String()))
		return manager.AuthorizeKitWorkload(ctx)
	}
	ctx, err := authorize(subject)
	require.NoError(t, err)
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)
	const sessionID = "kit-inference-session"
	_, err = queries.InsertKitRuntimeSession(t.Context(), assistantrepo.InsertKitRuntimeSessionParams{SessionID: sessionID, RuntimeID: runtime, ProjectID: project, AssistantID: assistant.ID, ThreadID: thread})
	require.NoError(t, err)
	_, err = manager.BindKitSession(ctx, chatID)
	require.Error(t, err, "a control-plane chat ID is not a guest session identity")
	_, err = manager.BindKitSession(ctx, uuid.New())
	require.Error(t, err, "unknown sessions must not acquire transcript authority")
	ctx, err = manager.BindKitSession(ctx, uuid.NewSHA1(uuid.NameSpaceDNS, []byte(sessionID)))
	require.NoError(t, err)
	boundChat, bound := assistanttokens.RuntimeChatID(ctx)
	require.True(t, bound)
	require.Equal(t, chatID, boundChat)
	require.True(t, assistanttokens.InferenceTranscriptOwned(ctx))
	for _, state := range []string{"failed", "expiring", "stopped"} {
		require.NoError(t, queries.SetAssistantRuntimeStateForTest(t.Context(), assistantrepo.SetAssistantRuntimeStateForTestParams{ProjectID: project, RuntimeID: runtime, State: state, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}))
		_, err = manager.BindKitSession(ctx, uuid.NewSHA1(uuid.NameSpaceDNS, []byte(sessionID)))
		require.Error(t, err, "non-live runtime %s must not bind session attribution", state)
	}
	require.NoError(t, queries.SetAssistantRuntimeStateForTest(t.Context(), assistantrepo.SetAssistantRuntimeStateForTestParams{ProjectID: project, RuntimeID: runtime, State: "active", UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}))

	// The trusted context bypasses both ingress and response persistence while
	// leaving chat identity available to telemetry; nil dependencies prove neither
	// branch can accidentally reach the canonical writer.
	capture := new(chat.ChatMessageCaptureStrategy)
	request := openrouter.CompletionRequest{ChatID: chatID, ProjectID: project.String(), OrgID: "org-test"}
	session, err := capture.StartOrResumeChat(ctx, request)
	require.NoError(t, err)
	require.Nil(t, session)
	require.NoError(t, capture.CaptureMessage(ctx, session, request, openrouter.CompletionResponse{}))
	require.Equal(t, chatID, request.ChatID)
	messages, err := chatrepo.New(db).CountChatMessages(t.Context(), chatrepo.CountChatMessagesParams{ProjectID: project, ChatID: chatID})
	require.NoError(t, err)
	require.Zero(t, messages)

	// Replacement metadata fences even an already-authorized request before binding.
	persist("runtime-generation-two")
	_, err = manager.BindKitSession(ctx, uuid.NewSHA1(uuid.NameSpaceDNS, []byte(sessionID)))
	require.Error(t, err)
	stale, err := authorize(subject)
	require.NoError(t, err)
	require.False(t, assistanttokens.InferenceTranscriptOwned(stale))
	_, err = manager.BindKitSession(stale, uuid.NewSHA1(uuid.NameSpaceDNS, []byte(sessionID)))
	require.Error(t, err)
	subject = "runtime-generation-two"
	ctx, err = authorize(subject)
	require.NoError(t, err)
	require.True(t, assistanttokens.InferenceTranscriptOwned(ctx))
	_, err = agentrepo.New(db).SuspendAgent(t.Context(), agentrepo.SuspendAgentParams{OrganizationID: "org-test", ID: resolution.AgentID})
	require.NoError(t, err)
	_, err = authorize(subject)
	require.Error(t, err, "current identity eligibility is checked on every request")
}
