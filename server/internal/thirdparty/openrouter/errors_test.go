package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
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

// echoedRequestCanary stands in for the request payload a provider echoes back
// when it rejects a request: transcript fragments, tool arguments, anything the
// caller sent.
const echoedRequestCanary = "echoed-request-payload-must-not-be-logged"

func TestChatClient_GetCompletion_ClassifiesHTTPErrorsBySanitizedBody(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name              string
		status            int
		body              string
		wantCorruption    bool
		wantBadRequest    bool
		wantContentPolicy bool
	}{
		{"400 anthropic orphaned tool_use", http.StatusBadRequest, `{"canary":"` + echoedRequestCanary + `","error":{"type":"invalid_request_error","message":"messages: tool_use_ids were found without corresponding tool_use blocks"}}`, true, false, false},
		{"422 role alternation", http.StatusUnprocessableEntity, `{"canary":"` + echoedRequestCanary + `","error":{"message":"messages must alternate between user and assistant"}}`, true, false, false},
		{"400 openai tool_calls pairing", http.StatusBadRequest, `{"canary":"` + echoedRequestCanary + `","error":{"message":"messages with role 'tool' must be a response to a preceeding message with 'tool_calls'"}}`, true, false, false},
		{"400 context overflow", http.StatusBadRequest, `{"canary":"` + echoedRequestCanary + `","error":{"message":"prompt is too long: 250000 tokens > 200000 maximum"}}`, true, false, false},
		{"400 invalid model param is NOT corruption", http.StatusBadRequest, `{"canary":"` + echoedRequestCanary + `","error":{"type":"invalid_request_error","message":"model 'gpt-7' does not exist"}}`, false, true, false},
		{"400 malformed json is NOT corruption", http.StatusBadRequest, `{"canary":"` + echoedRequestCanary + `","error":{"message":"invalid JSON in request body"}}`, false, true, false},
		{"422 unsupported schema is a plain bad request", http.StatusUnprocessableEntity, `{"canary":"` + echoedRequestCanary + `","error":{"message":"response_format is not supported by this model"}}`, false, true, false},
		{"401 auth is neither", http.StatusUnauthorized, `{"canary":"` + echoedRequestCanary + `","error":{"message":"no auth credentials found"}}`, false, false, false},
		{"402 credits is neither", http.StatusPaymentRequired, `{"canary":"` + echoedRequestCanary + `","error":{"message":"insufficient credits"}}`, false, false, false},
		{"403 moderation is a content policy rejection", http.StatusForbidden, `{"canary":"` + echoedRequestCanary + `","error":{"type":"content_policy_violation"}}`, false, false, true},
		{"403 openrouter moderation envelope is a content policy rejection", http.StatusForbidden, `{"canary":"` + echoedRequestCanary + `","error":{"code":403,"message":"Your chosen model requires moderation and your input was flagged","metadata":{"reasons":["harassment"],"flagged_input":"..."}}}`, false, false, true},
		{"403 provider safety block is a content policy rejection", http.StatusForbidden, `{"canary":"` + echoedRequestCanary + `","error":{"message":"the response was blocked due to safety settings"}}`, false, false, true},
		{"403 forbidden key is NOT a content policy rejection", http.StatusForbidden, `{"canary":"` + echoedRequestCanary + `","error":{"message":"forbidden: key does not have access to this model"}}`, false, false, false},
		{"403 auth is NOT a content policy rejection", http.StatusForbidden, `{"canary":"` + echoedRequestCanary + `","error":{"message":"no auth credentials found"}}`, false, false, false},
		// A 403 body echoes the request back, so the transcript's own words must
		// not decide the classification: an auth failure turned content-policy is
		// terminal and burns the caller's whole attempt budget.
		{"403 auth echoing a transcript about safety is NOT a content policy rejection", http.StatusForbidden, `{"canary":"` + echoedRequestCanary + `","error":{"message":"no auth credentials found"},"request":{"messages":[{"role":"user","content":"review the safety and moderation copy, the reviewer flagged it"}]}}`, false, false, false},
		{"403 unentitled key echoing model safety wording is NOT a content policy rejection", http.StatusForbidden, `{"canary":"` + echoedRequestCanary + `","error":{"message":"key does not have access to model llama-guard-safety"}}`, false, false, false},
		{"408 timeout is neither", http.StatusRequestTimeout, `{"canary":"` + echoedRequestCanary + `","error":{"message":"request timed out"}}`, false, false, false},
		{"409 conflict is neither", http.StatusConflict, `{"canary":"` + echoedRequestCanary + `","error":{"message":"conflict"}}`, false, false, false},
		{"429 rate limit is NOT corruption", http.StatusTooManyRequests, `{"canary":"` + echoedRequestCanary + `","error":{"message":"rate_limit_exceeded"}}`, false, false, false},
		{"500 upstream blip is NOT corruption", http.StatusInternalServerError, `{"canary":"` + echoedRequestCanary + `","error":{"message":"upstream"}}`, false, false, false},
		{"503 upstream unavailable is neither", http.StatusServiceUnavailable, `{"canary":"` + echoedRequestCanary + `","error":{"message":"no available provider"}}`, false, false, false},
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
			require.Equal(t, tc.wantBadRequest, IsBadRequest(err))
			require.Equal(t, tc.wantContentPolicy, IsContentPolicy(err))

			require.NotContains(t, err.Error(), echoedRequestCanary, "response body must not reach the error message for any status")
			require.Contains(t, err.Error(), strconv.Itoa(tc.status), "status must survive sanitization")
		})
	}
}

// The body is kept out of the error but has to survive somewhere, or a
// misclassified status can only be diagnosed by guessing.
func TestClassifyHTTPError_RecordsBoundedBodyOnSpan(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(t.Context(), "completion")

	body := []byte(`{"error":{"message":"no auth credentials found"},"echo":"` + strings.Repeat("é", 4000) + `"}`)
	err := classifyHTTPError(ctx, http.StatusForbidden, body)
	span.End()

	require.Error(t, err)
	require.NotContains(t, err.Error(), "no auth credentials found")

	spans := recorder.Ended()
	require.Len(t, spans, 1)

	var recorded string
	var status int64
	for _, kv := range spans[0].Attributes() {
		switch kv.Key {
		case attr.OpenRouterResponseBodyKey:
			recorded = kv.Value.AsString()
		case attr.HTTPResponseStatusCodeKey:
			status = kv.Value.AsInt64()
		}
	}

	require.Equal(t, int64(http.StatusForbidden), status)
	require.Contains(t, recorded, "no auth credentials found")
	require.LessOrEqual(t, len(recorded), maxDiagnosticBodyBytes)
	require.True(t, utf8.ValidString(recorded), "a truncated body must stay valid UTF-8")
}

func TestChatClient_GetCompletion_EmptyChoices_EmbedsResponseBody(t *testing.T) {
	t.Parallel()

	const body = `{"id":"gen-abc123","model":"anthropic/claude","choices":[],"error":{"code":429,"message":"provider rate limited"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	client := newTestClientForServer(t, server)
	_, err := client.GetCompletion(t.Context(), CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   uuid.New().String(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "gen-abc123")            // generation id surfaced for OpenRouter lookup
	require.Contains(t, err.Error(), "provider rate limited") // embedded upstream error surfaced verbatim
}

func TestChatClient_GetCompletion_RetriesEmptyChoicesThenSucceeds(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"id":"gen-empty","choices":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"gen-ok","model":"m","choices":[{"message":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)

	client := newTestClientForServer(t, server)
	resp, err := client.GetCompletion(t.Context(), CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   uuid.New().String(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	})
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load()) // retried once after the empty response
	require.Equal(t, "recovered", resp.Content)
}

func TestChatClient_GetCompletion_RetriesExhaustedReturnsError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"gen-empty","choices":[]}`))
	}))
	t.Cleanup(server.Close)

	client := newTestClientForServer(t, server)
	_, err := client.GetCompletion(t.Context(), CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   uuid.New().String(),
		Messages:    []or.ChatMessages{CreateMessageUser("hi")},
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	})
	require.Error(t, err)
	require.Equal(t, int32(2), calls.Load()) // both attempts exhausted
	require.Contains(t, err.Error(), "after 2 attempts")
	require.Contains(t, err.Error(), "gen-empty")
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
		&PlatformKeyResolver{Provisioner: &mockProvisioner{apiKey: "test-api-key"}},
		&mockMessageCaptureStrategy{},
		&mockUsageTrackingStrategy{},
		&mockChatTitleGenerator{},
		&mockTelemetryLogger{},
	)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}
	return client
}
