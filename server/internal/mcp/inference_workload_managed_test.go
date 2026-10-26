package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/stretchr/testify/require"
)

// This test uses a signed external assertion, the real JWT exchange and live
// workload authorization, then the public completion handler. Only the model
// transport is stubbed; it exercises the actual capture-ownership strategy.
func TestWorkloadInference_ManagedCompletion(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	ctx := t.Context()
	issuer := f.sharedIssuer(t)
	q := assistantrepo.New(f.ti.conn)
	project := f.fx.target.ProjectID
	a, err := q.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{ProjectID: project, OrganizationID: f.fx.orgID, CreatedByUserID: conv.ToPGText(f.fx.userID), Name: "Managed completion", Model: "test/model", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active"})
	require.NoError(t, err)
	trigger, err := triggerrepo.New(f.ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{OrganizationID: f.fx.orgID, ProjectID: project, DefinitionSlug: "manual", Name: "Managed root", TargetKind: "assistant", TargetRef: a.ID.String(), TargetDisplay: a.Name, ConfigJson: []byte(`{}`), Status: "active"})
	require.NoError(t, err)
	identities := assistantidentity.New(f.ti.serverURL.String(), f.ti.audit)
	tx := testenv.BeginTx(t, ctx, f.ti.conn)
	require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{OrganizationID: f.fx.orgID, ProjectID: project, AssistantID: a.ID, ActorUserID: f.fx.userID}))
	require.NoError(t, tx.Commit(ctx))
	identity, err := identities.Resolve(ctx, f.ti.conn, f.fx.orgID, project, a.ID, trigger.ID)
	require.NoError(t, err)
	require.NotNil(t, identity.Identity)
	// The generic workload fixture starts with an organization-wide agent.
	// Provision a dedicated assistant agent through the real identity service,
	// then assign the projected workload to that exact project/owner identity.
	unassignWorkloadAgent(t, ctx, f.ti, f.assignmentID)
	f.agentID = identity.Identity.AgentID
	f.assignmentID = assignAgentToWorkload(t, ctx, f.ti, f.fx.orgID, f.issuerID, f.subject, f.agentID)
	agentGrant, ownerGrant := enableWorkloadInference(t, f)

	chatID := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{ChatID: chatID, ProjectID: project, OrganizationID: f.fx.orgID, UserID: conv.ToPGText(f.fx.userID)}))
	thread, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{AssistantID: a.ID, ProjectID: project, CorrelationID: "managed-inference", ChatID: chatID, SourceKind: "dashboard", SourceRefJson: []byte(`{}`)})
	require.NoError(t, err)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: a.ID, RuntimeVersion: 3}))
	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: a.ID, AssistantThreadID: thread, Backend: "local", State: "active"}))
	runtime, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: a.ID})
	require.NoError(t, err)
	persist := func(subject string) {
		t.Helper()
		metadata, err := json.Marshal(map[string]any{"workload": map[string]any{"issuer_id": f.issuerID, "subject": subject, "identity": identity.Identity}})
		require.NoError(t, err)
		require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: project, RuntimeID: runtime.ID, BackendMetadataJson: metadata}))
	}
	persist(f.subject)
	const sessionID = "managed-completion-acp-session"
	_, err = q.InsertKitRuntimeSession(ctx, assistantrepo.InsertKitRuntimeSessionParams{SessionID: sessionID, RuntimeID: runtime.ID, ProjectID: project, AssistantID: a.ID, ThreadID: thread})
	require.NoError(t, err)
	token := requireWorkloadInferenceToken(t, f, issuer)
	manager := assistanttokens.New("test-secret", f.ti.conn, f.ti.authzEngine, nil, nil)
	calls := 0
	client := &managedInferenceCompletion{complete: func(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
		calls++
		require.Equal(t, billing.ModelUsageSourceAssistants, request.UsageSource)
		require.Equal(t, billing.ModelUsageSourceAssistants, request.KeySlot)
		require.Equal(t, chatID, request.ChatID)
		require.Empty(t, request.APIKeyID)
		principal, ok := contextvalues.GetAssistantPrincipal(ctx)
		require.True(t, ok)
		require.Equal(t, a.ID, principal.AssistantID)
		require.Equal(t, thread, principal.ThreadID)
		require.True(t, assistanttokens.InferenceTranscriptOwned(ctx))
		capture := new(chat.ChatMessageCaptureStrategy)
		session, err := capture.StartOrResumeChat(ctx, request)
		require.NoError(t, err)
		require.Nil(t, session)
		require.NoError(t, capture.CaptureMessage(ctx, session, request, openrouter.CompletionResponse{}))
		message := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{})
		return &openrouter.CompletionResponse{Message: &message, Model: "test/model"}, nil
	}}
	logger, tracer := testenv.NewLogger(t), testenv.NewTracerProvider(t)
	svc := chat.NewService(logger, tracer, f.ti.conn, f.ti.sessionManager, f.ti.chatSessionsManager, nil, client, nil, posthog.New(ctx, logger, "", "", ""), nil, nil, f.ti.authzEngine, manager, billing.NewStubClient(logger, tracer), f.ti.audit).WithWorkloadInference(f.ti.service)
	request := func(user string) *http.Request {
		body, err := json.Marshal(map[string]any{"model": "test/model", "messages": []any{}, "user": user})
		require.NoError(t, err)
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/chat/completions", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		// A spoofed exemption cannot change trusted assistant billing.
		r.Header.Set("X-Gram-Source", "gram")
		return r
	}
	require.NoError(t, svc.HandleCompletion(httptest.NewRecorder(), request(sessionID)))
	require.Equal(t, 1, calls)
	count, err := chatrepo.New(f.ti.conn).CountChatMessages(ctx, chatrepo.CountChatMessagesParams{ProjectID: project, ChatID: chatID})
	require.NoError(t, err)
	require.Zero(t, count)
	require.Error(t, svc.HandleCompletion(httptest.NewRecorder(), request("unbound-session")))
	// Mutation occurs after real authorization, as the body is first read.
	racing := request(sessionID)
	racing.Body = &managedInferenceReadHook{ReadCloser: racing.Body, before: func() { persist("replacement-generation") }}
	require.Error(t, svc.HandleCompletion(httptest.NewRecorder(), racing))
	require.Equal(t, 1, calls, "rotation must fail before reaching the model")
	persist(f.subject)
	for _, grant := range []uuid.UUID{agentGrant, ownerGrant} {
		_, err := accessrepo.New(f.ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: grant, OrganizationID: f.fx.orgID})
		require.NoError(t, err)
		require.Error(t, svc.HandleCompletion(httptest.NewRecorder(), request(sessionID)), "managed attribution cannot bypass live policy")
		enableWorkloadInference(t, f)
	}
	// Retired metadata alone gives no managed authority; revoked admission must
	// reject the same still-unexpired bearer before ordinary-workload fallback.
	persist("replacement-generation")
	_, err = testrepo.New(f.ti.conn).SoftDeleteWorkloadAdmissionFixture(ctx, testrepo.SoftDeleteWorkloadAdmissionFixtureParams{OrganizationID: f.fx.orgID, WorkloadIssuerID: f.issuerID, Subject: f.subject})
	require.NoError(t, err)
	require.Error(t, svc.HandleCompletion(httptest.NewRecorder(), request(sessionID)))
	require.Equal(t, 1, calls)
}

type managedInferenceCompletion struct {
	openrouter.CompletionClient
	complete func(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error)
}

func (c *managedInferenceCompletion) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	return c.complete(ctx, request)
}

type managedInferenceReadHook struct {
	io.ReadCloser
	before func()
}

func (r *managedInferenceReadHook) Read(p []byte) (int, error) {
	if r.before != nil {
		r.before()
		r.before = nil
	}
	n, err := r.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read managed inference body: %w", err)
	}
	return n, nil
}
