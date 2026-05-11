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
