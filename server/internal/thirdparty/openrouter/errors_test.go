package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatClient_GetCompletion_402_WrapsInsufficientCredits(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"This request requires more credits, or fewer max_tokens."}}`))
	}))
	defer t.Cleanup(server.Close)

	client := newTestClientForServer(t, server)

	_, err := client.GetCompletion(context.Background(), CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   uuid.New().String(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInsufficientCredits)
	assert.True(t, IsInsufficientCredits(err))
}

func TestChatClient_GetCompletionStream_402_WrapsInsufficientCredits(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient credits"}}`))
	}))
	defer t.Cleanup(server.Close)

	client := newTestClientForServer(t, server)

	_, err := client.GetCompletionStream(context.Background(), CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   uuid.New().String(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	})
	require.Error(t, err)
	assert.True(t, IsInsufficientCredits(err))
}

func TestChatClient_GetCompletion_Non402_DoesNotWrapInsufficientCredits(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream error"}}`))
	}))
	defer t.Cleanup(server.Close)

	client := newTestClientForServer(t, server)

	_, err := client.GetCompletion(context.Background(), CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   uuid.New().String(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	})
	require.Error(t, err)
	assert.False(t, IsInsufficientCredits(err), "non-402 must not be classified as insufficient credits")
}

func TestChatClient_GetCompletion_WrapsHistoryCorruptionForBadRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		status         int
		body           string
		wantCorruption bool
	}{
		{"400 anthropic orphaned tool_use", http.StatusBadRequest, `{"error":{"type":"invalid_request_error","message":"messages: tool_use_ids were found without corresponding tool_use blocks"}}`, true},
		{"422 role alternation", http.StatusUnprocessableEntity, `{"error":{"message":"messages must alternate between user and assistant"}}`, true},
		{"400 openai tool_calls pairing", http.StatusBadRequest, `{"error":{"message":"messages with role 'tool' must be a response to a preceeding message with 'tool_calls'"}}`, true},
		{"400 context overflow", http.StatusBadRequest, `{"error":{"message":"prompt is too long: 250000 tokens > 200000 maximum"}}`, true},
		{"400 invalid model param is NOT corruption", http.StatusBadRequest, `{"error":{"type":"invalid_request_error","message":"model 'gpt-7' does not exist"}}`, false},
		{"400 malformed json is NOT corruption", http.StatusBadRequest, `{"error":{"message":"invalid JSON in request body"}}`, false},
		{"403 moderation is NOT corruption", http.StatusForbidden, `{"error":{"type":"content_policy_violation"}}`, false},
		{"429 rate limit is NOT corruption", http.StatusTooManyRequests, `{"error":{"message":"rate_limit_exceeded"}}`, false},
		{"500 upstream blip is NOT corruption", http.StatusInternalServerError, `{"error":{"message":"upstream"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)

			client := newTestClientForServer(t, server)
			_, err := client.GetCompletion(context.Background(), CompletionRequest{
				OrgID:       "test-org",
				ProjectID:   uuid.New().String(),
				Messages:    []or.ChatMessages{CreateMessageUser("hi")},
				ChatID:      uuid.New(),
				UsageSource: billing.ModelUsageSourcePlayground,
			})
			require.Error(t, err)
			require.Equal(t, tc.wantCorruption, IsHistoryCorruptionCandidate(err))
		})
	}
}

func newTestClientForServer(t *testing.T, server *httptest.Server) *ChatClient {
	t.Helper()

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	client := NewUnifiedClient(
		testenv.NewLogger(t),
		guardianPolicy,
		&mockProvisioner{apiKey: "test-api-key"},
		&mockMessageCaptureStrategy{},
		&mockUsageTrackingStrategy{},
		&mockChatTitleGenerator{},
		&mockChatResolutionAnalyzer{},
		&mockTelemetryLogger{},
	)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}
	return client
}
