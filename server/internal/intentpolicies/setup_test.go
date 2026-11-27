package intentpolicies_test

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	intentpolicyv1 "github.com/speakeasy-api/gram/infra/gen/gram/intentpolicy/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/intentpolicies"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// geminiStub answers object completions with canned label sets by prompt content.
type geminiStub struct {
	openrouter.CompletionClient
	body string
}

func (g geminiStub) GetObjectCompletion(_ context.Context, req openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	body := g.body
	if strings.Contains(req.Prompt, "git push --force") {
		body = geminiRemoteDestruction
	}
	content := or.CreateChatAssistantMessageContentStr(body)
	msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
	cost := 0.002
	return &openrouter.CompletionResponse{
		StartTime:    time.Time{},
		Message:      &msg,
		MessageID:    "msg_test",
		Model:        "test-model",
		Usage:        openrouter.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, Cost: &cost},
		FinishReason: nil,
		ToolCalls:    nil,
		Content:      body,
	}, nil
}

const geminiBenign = `{
	"effect_class": {"value": ["read"], "confidence": 0.9, "rationale": "Lists files."},
	"target_locations": {"value": ["workspace"], "confidence": 0.9, "rationale": "Workspace."},
	"payload_data": {"value": [], "confidence": 0.9, "rationale": "None."},
	"exec_code_origin": {"value": "none", "confidence": 0.9, "rationale": "Not exec."},
	"audience": {"value": "none", "confidence": 0.9, "rationale": "Not outbound."},
	"effect_coverage": {"value": "understood", "confidence": 0.9, "rationale": "Clear."}
}`

const geminiRemoteDestruction = `{
	"effect_class": {"value": ["destructive", "outbound"], "confidence": 0.9, "rationale": "Force-pushes a remote branch."},
	"target_locations": {"value": ["remote"], "confidence": 0.9, "rationale": "Targets remote git ref."},
	"payload_data": {"value": [], "confidence": 0.9, "rationale": "None."},
	"exec_code_origin": {"value": "none", "confidence": 0.9, "rationale": "Not exec."},
	"audience": {"value": "group", "confidence": 0.9, "rationale": "Shared repository."},
	"effect_coverage": {"value": "understood", "confidence": 0.9, "rationale": "Clear."}
}`

func newTestService(t *testing.T, jev classifier.Classifier) (context.Context, *intentpolicies.Service) {
	t.Helper()
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagIntentPolicies, feature.AnyDistinctID, true)
	return newTestServiceWithFlags(t, jev, flags)
}

func newTestServiceWithFlags(t *testing.T, jev classifier.Classifier, flags feature.Provider) (context.Context, *intentpolicies.Service) {
	t.Helper()
	ctx, svc, _, _ := newTestServiceWith(t, jev, flags, false)
	return ctx, svc
}

// newTestServiceWithShadows also returns the database. With shadows, the
// caller's project keeps encrypted transcript shadows: uploads capture them
// and analysis reads through them.
func newTestServiceWithShadows(t *testing.T, jev classifier.Classifier, flags feature.Provider, shadows bool) (context.Context, *intentpolicies.Service, *pgxpool.Pool) {
	t.Helper()
	ctx, svc, conn, _ := newTestServiceWith(t, jev, flags, shadows)
	return ctx, svc, conn
}

// recordingPublisher captures published assessments and settles every publish
// successfully.
type recordingPublisher struct {
	mu       sync.Mutex
	messages []*intentpolicyv1.Assessment
}

func (p *recordingPublisher) Publish(_ context.Context, msg *intentpolicyv1.Assessment, _ ...gcp.PublishOption) gcp.PublishResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, msg)
	return gcp.NewErrPublishResult(nil)
}

func (p *recordingPublisher) Stop(context.Context) error { return nil }

func (p *recordingPublisher) published() []*intentpolicyv1.Assessment {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*intentpolicyv1.Assessment(nil), p.messages...)
}

func newTestServiceWithResults(t *testing.T, jev classifier.Classifier, flags feature.Provider) (context.Context, *intentpolicies.Service, *recordingPublisher) {
	t.Helper()
	ctx, svc, _, results := newTestServiceWith(t, jev, flags, false)
	return ctx, svc, results
}

func newTestServiceWith(t *testing.T, jev classifier.Classifier, flags feature.Provider, shadows bool) (context.Context, *intentpolicies.Service, *pgxpool.Pool, *recordingPublisher) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "intent_policies_api")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 12)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	// Analyzing reads chat transcripts and uploading writes a chat; neither
	// comes with the Admin role.
	ctx = authztest.WithAdminGrants(authztest.InitAuthContext(t, ctx, conn, sessionManager),
		authz.NewGrant(authz.ScopeChatRead, authz.WildcardResource),
		authz.NewGrant(authz.ScopeChatWrite, authz.WildcardResource),
	)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	results := &recordingPublisher{mu: sync.Mutex{}, messages: nil}
	writer, shutdown := chat.NewChatMessageWriter(logger, conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	svc, err := intentpolicies.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, flags, writer, geminiStub{CompletionClient: nil, body: geminiBenign}, jev, results)
	require.NoError(t, err)
	if !shadows {
		return ctx, svc, conn, results
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	keys := orgencryptiontest.New(t, conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(ctx, authCtx.ActiveOrganizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	store, err := transcriptstore.New(conn, keys.Service, logger, tracerProvider, testenv.NewMeterProvider(t))
	require.NoError(t, err)
	transcripts := transcriptstore.NewTranscripts(store, *authCtx.ProjectID)
	writer.WithTranscripts(transcripts)
	svc.WithTranscripts(transcripts)
	return ctx, svc, conn, results
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
