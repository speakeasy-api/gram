package intentpolicies_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/intentpolicies"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

// geminiStub answers every object completion with a fixed JSON body.
type geminiStub struct {
	openrouter.CompletionClient
	body string
}

func (g geminiStub) GetObjectCompletion(context.Context, openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	content := or.CreateChatAssistantMessageContentStr(g.body)
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
		Content:      g.body,
	}, nil
}

const geminiBenign = `{
	"effect_class": {"value": ["read"], "confidence": 0.9, "rationale": "Lists files."},
	"carries_secret": {"value": false, "confidence": 0.9, "rationale": "None."},
	"executes_remote_code_inline": {"value": false, "confidence": 0.9, "rationale": "None."},
	"touches_credential_store": {"value": false, "confidence": 0.9, "rationale": "None."},
	"touches_persistence": {"value": false, "confidence": 0.9, "rationale": "None."},
	"history_rewrite": {"value": false, "confidence": 0.9, "rationale": "None."},
	"carries_personal_data": {"value": false, "confidence": 0.9, "rationale": "None."},
	"destructive_target": {"value": "none", "confidence": 0.9, "rationale": "Not destructive."},
	"publishes_artifact": {"value": false, "confidence": 0.9, "rationale": "None."},
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

	writer, shutdown := chat.NewChatMessageWriter(logger, conn, nil)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	svc, err := intentpolicies.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, flags, writer, geminiStub{CompletionClient: nil, body: geminiBenign}, jev)
	require.NoError(t, err)
	return ctx, svc
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
