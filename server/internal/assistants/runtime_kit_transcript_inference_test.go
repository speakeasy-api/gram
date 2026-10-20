package assistants

import (
	"context"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/auth"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
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
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	trigger, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, assistant.ID, assistant.Name)
	require.NoError(t, err)
	resolution, err := core.identities.Resolve(t.Context(), db, "org-test", project, assistant.ID, trigger)
	require.NoError(t, err)
	require.NotNil(t, resolution.Identity)
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
	issue := func() assistanttokens.KitAPIKey {
		t.Helper()
		require.NoError(t, manager.RevokeRuntimeInferenceCredentials(t.Context(), project, runtime))
		credential, err := manager.IssueKitAPIKey(t.Context(), assistanttokens.InferenceIssueInput{Identity: *resolution.Identity, RuntimeID: runtime, Lifetime: 24 * time.Hour}, "test")
		require.NoError(t, err)
		return credential
	}
	// Build the principal context from the native key's persisted issuance ceiling.
	// Full HTTP API-key authentication is covered by completion_kit_auth_test.go.
	authorize := func(key assistanttokens.KitAPIKey) (context.Context, error) {
		hash, err := auth.GetAPIKeyHash(key.Key)
		if err != nil {
			return t.Context(), err
		}
		row, err := keysrepo.New(db).GetAPIKeyByKeyHash(t.Context(), hash)
		if err != nil {
			return t.Context(), err
		}
		ac := &contextvalues.AuthContext{APIKeyID: key.ID.String(), ActiveOrganizationID: "org-test", ProjectID: &project}
		ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), ac, urn.NewPrincipal(urn.PrincipalTypeAgent, resolution.Identity.AgentID.String()), contextvalues.PrincipalCredential{AuthorizerUserID: row.CreatedByUserID, DelegatedGrants: row.DelegatedGrants, DelegatedGrantsVersion: row.DelegatedGrantsVersion.Int32})
		ctx, err = engine.PrepareContext(ctx)
		if err != nil {
			return ctx, fmt.Errorf("prepare authorization context: %w", err)
		}
		ctx, _, err = manager.AuthorizeKitAPIKey(ctx)
		if err != nil {
			return ctx, fmt.Errorf("authorize Kit API key: %w", err)
		}
		return ctx, nil
	}
	credential := issue()
	ctx, err := authorize(credential)
	require.NoError(t, err)
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeAgent, actor.Type)
	principal, ok := contextvalues.PrincipalCredentialAuthorization(ctx)
	require.True(t, ok)
	require.Equal(t, "user-1", principal.AuthorizerUserID)
	_, _, err = manager.Authorize(t.Context(), credential.Key)
	require.Error(t, err, "inference bearer cannot authorize transcript/legacy APIs")
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
	for _, state := range []string{"failed", "expired", "stopped"} {
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
	// Runtime-wide revoke is the restore/startup rotation boundary.
	require.NoError(t, manager.RevokeRuntimeInferenceCredentials(t.Context(), project, runtime))
	_, err = authorize(credential)
	require.Error(t, err)
	credential = issue()
	binding, err := tokenrepo.New(db).ReadKitAPIKeyBinding(t.Context(), credential.ID)
	require.NoError(t, err)
	require.NoError(t, testrepo.New(db).ExpireAssistantInferenceCredentialForTest(t.Context(), testrepo.ExpireAssistantInferenceCredentialForTestParams{OrganizationID: "other-org", ProjectID: project, ID: binding.ID}))
	_, err = authorize(credential)
	require.NoError(t, err, "another organization cannot expire the native key")
	err = testrepo.New(db).ExpireAssistantInferenceCredentialForTest(t.Context(), testrepo.ExpireAssistantInferenceCredentialForTestParams{OrganizationID: "org-test", ProjectID: project, ID: binding.ID})
	require.NoError(t, err)
	_, err = authorize(credential)
	require.Error(t, err, "persisted expiry is enforced")
	credential = issue()
	_, err = agentrepo.New(db).SuspendAgent(t.Context(), agentrepo.SuspendAgentParams{OrganizationID: "org-test", ID: resolution.Identity.AgentID})
	require.NoError(t, err)
	_, err = authorize(credential)
	require.Error(t, err, "live workload eligibility is checked on every request")
	_, err = agentrepo.New(db).ResumeAgent(t.Context(), agentrepo.ResumeAgentParams{OrganizationID: "org-test", ID: resolution.Identity.AgentID})
	require.NoError(t, err)
	ctx, err = authorize(credential)
	require.NoError(t, err)
	check := authz.MCPCheck(authz.ScopeMCPConnect, uuid.NewString(), project.String())
	require.NoError(t, engine.Require(ctx, check))
	deleteAgentScope(t, db, resolution.Identity.AgentID.String(), authz.ScopeMCPConnect)
	deleteAgentScope(t, db, resolution.Identity.AgentID.String(), authz.ScopeMCPRead)
	ctx, err = authorize(credential)
	require.NoError(t, err)
	require.Error(t, engine.Require(ctx, check), "live policy narrows the persisted issuance ceiling")
	require.NoError(t, manager.RevokeInferenceCredential(t.Context(), project, credential.ID))
	_, err = authorize(credential)
	require.Error(t, err)
}
