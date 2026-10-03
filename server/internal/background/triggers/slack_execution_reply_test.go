package triggers_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

type denialBudget struct {
	cache.Cache
	mu   sync.Mutex
	sent map[string]bool
}

func (b *denialBudget) Add(_ context.Context, key string, _ time.Duration) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sent[key] {
		return false, nil
	}
	b.sent[key] = true
	return true, nil
}

type denialEnvironment struct{}

func (denialEnvironment) Load(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error) {
	return map[string]string{"SLACK_BOT_TOKEN": "xoxb-example", "SLACK_SIGNING_SECRET": "example-secret"}, nil
}

func TestAssistantSlackDenialReplyUsesExactOriginAndStaticPrivateContent(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postEphemeral" {
			t.Errorf("unexpected Slack API: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse Slack request: %v", err)
		}
		if r.Form.Get("channel") != "CEXAMPLE" || r.Form.Get("user") != "UEXAMPLE" || r.Form.Get("thread_ts") != "123" {
			t.Error("wrong reply recipient")
		}
		if r.Form.Get("text") != "I couldn't access the business tools for this request. Please check your permissions and connected accounts." {
			t.Error("non-static refusal content")
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse("https://example.invalid")
	require.NoError(t, err)
	app := triggers.NewApp(testenv.NewLogger(t), f.db, nil, denialEnvironment{}, nil, nil, base, base, nil, slackclient.NewSlackClientWithBaseURL(server.URL, server.Client()), &denialBudget{Cache: cache.NoopCache, sent: map[string]bool{}}).SetIdentityService(testIdentityService)
	p := f.createParams()
	p.DefinitionSlug = triggers.DefinitionSlugSlack
	root, err := app.Create(t.Context(), p)
	require.NoError(t, err)
	identity, err := testIdentityService.Resolve(t.Context(), f.db, "org-trigger-test", f.projectID, f.assistantID, root.ID)
	require.NoError(t, err)
	ceiling, err := testIdentityService.SnapshotCeiling(t.Context(), f.db, *identity.Identity)
	require.NoError(t, err)
	q := assistantrepo.New(f.db)
	chat := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(t.Context(), assistantrepo.UpsertAssistantChatParams{ChatID: chat, ProjectID: f.projectID, OrganizationID: "org-trigger-test", UserID: conv.ToPGText("trigger-owner"), Title: conv.ToPGText("Shared conversation")}))
	thread, err := q.UpsertAssistantThread(t.Context(), assistantrepo.UpsertAssistantThreadParams{AssistantID: f.assistantID, ProjectID: f.projectID, CorrelationID: "slack-original", ChatID: chat, SourceKind: "slack", SourceRefJson: []byte(`{}`)})
	require.NoError(t, err)
	execution := assistantidentity.Execution{Version: assistantidentity.ExecutionVersion, Identity: *identity.Identity, Issuer: testIdentityService.Issuer(), ThreadID: thread, EventID: "origin", Mode: assistantidentity.ExecutionWorkloadHuman, HumanUserID: "trigger-owner", Ceiling: ceiling, Slack: &assistantidentity.SlackDelegation{TeamID: "TEXAMPLE", UserID: "UEXAMPLE", MembershipID: uuid.New(), MappingID: uuid.New(), MappingRevision: 1, ConnectionGeneration: uuid.New()}}
	require.NoError(t, identityrepo.New(f.db).FixtureSlackExecutionMapping(t.Context(), identityrepo.FixtureSlackExecutionMappingParams{UserID: "trigger-owner", OrganizationID: "org-trigger-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE", Generation: uuid.New()}))
	execution.Slack, _, _, err = assistantidentity.CaptureSlackDelegation(t.Context(), f.db, "org-trigger-test", "TEXAMPLE", "UEXAMPLE")
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{"_gram_execution": execution, "team_id": "TEXAMPLE", "user_id": "UEXAMPLE", "channel_id": "CEXAMPLE", "thread_id": "123"})
	require.NoError(t, err)
	_, err = q.InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: thread, AssistantID: f.assistantID, ProjectID: f.projectID, TriggerInstanceID: conv.ToNullUUID(root.ID), EventID: "origin", CorrelationID: "slack-original", Status: "processing", NormalizedPayloadJson: payload, SourcePayloadJson: []byte(`{}`)})
	require.NoError(t, err)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	signer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), testIdentityService.Issuer(), false)
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), f.db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{AdmitWorkloadSession: runtimepolicy.AdmitWorkloadSession})
	manager := assistanttokens.New("test-secret", f.db, engine)
	manager.ConfigureExecutionIdentity(signer, testIdentityService)
	token, err := manager.GenerateExecution(t.Context(), execution)
	require.NoError(t, err)
	require.NoError(t, slackrepo.New(f.db).RevokeSlackIdentityMapping(t.Context(), slackrepo.RevokeSlackIdentityMappingParams{OrganizationID: "org-trigger-test", SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE"}))
	_, err = manager.AuthorizeBusiness(t.Context(), token, uuid.New(), nil)
	require.Error(t, err, "selected human revocation denies business access")
	ctx, _, err := manager.AuthorizePlatform(t.Context(), token)
	require.NoError(t, err, "assistant-owned refusal remains authorized")
	// No mapping is live in this fixture: refusal delivery does not need human
	// business authority and cannot disclose any business result.
	require.NoError(t, app.NotifyAssistantExecutionDenied(ctx))
	var concurrent sync.WaitGroup
	for range 8 {
		concurrent.Go(func() {
			if err := app.NotifyAssistantExecutionDenied(ctx); err == nil {
				t.Error("repeat refusal must not claim another send")
			}
		})
	}
	concurrent.Wait()
	require.Equal(t, int32(1), calls.Load(), "duplicate calls share one atomic send budget")
	require.Error(t, app.NotifyAssistantExecutionDenied(contextvalues.WithAssistantInvocationEvent(ctx, "wrong-event")))
	wrong := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: thread})
	require.Error(t, app.NotifyAssistantExecutionDenied(wrong))
	require.Equal(t, int32(1), calls.Load())
	execution.Slack = nil
	execution.Mode = assistantidentity.ExecutionWorkload
	execution.HumanUserID = ""
	execution.FallbackReason = "slack_mapping_absent"
	execution.EventID = "unmapped-origin"
	payload, err = json.Marshal(map[string]any{"_gram_execution": execution, "team_id": "TEXAMPLE", "user_id": "UEXAMPLE", "channel_id": "CEXAMPLE", "thread_id": "123"})
	require.NoError(t, err)
	_, err = q.InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{AssistantThreadID: thread, AssistantID: f.assistantID, ProjectID: f.projectID, TriggerInstanceID: conv.ToNullUUID(root.ID), EventID: execution.EventID, CorrelationID: "slack-original", Status: "processing", NormalizedPayloadJson: payload, SourcePayloadJson: []byte(`{}`)})
	require.NoError(t, err)
	token, err = manager.GenerateExecution(t.Context(), execution)
	require.NoError(t, err)
	ctx, _, err = manager.AuthorizePlatform(t.Context(), token)
	require.NoError(t, err)
	require.NoError(t, app.NotifyAssistantExecutionDenied(ctx), "trusted unmapped Slack origin can receive a constant refusal")
	require.Equal(t, int32(2), calls.Load())
	require.NoError(t, app.SetAssistantThreadRouteState(ctx, triggers.AssistantThread{ProjectID: f.projectID, AssistantID: f.assistantID, ThreadID: thread}, triggers.ThreadRouteUnsubscribed))
}
