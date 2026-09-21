package llmanalyzer_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testModel  = "risk-judge-test"
	testAPIKey = "test-api-key"
)

var testInfo = llmanalyzer.CallInfo{OrgID: "org-1", OrgSlug: "acme-test", ScanMode: llmanalyzer.ScanModeSync}

// cleanVerdict is a well-formed model reply with nothing flagged.
const cleanVerdict = `{"secrets_leak": {"score": 0, "reasoning": "none"}, "personal_data_leak": {"score": 0, "reasoning": "none"},` +
	` "prompt_injection": {"score": 0, "reasoning": "none"}, "destructive_tool_call": {"score": 0, "reasoning": "none"}}`

// capturedRequest is what the fake upstream saw, recorded for assertions on
// the test goroutine.
type capturedRequest struct {
	method        string
	path          string
	authorization string
	contentType   string
	body          map[string]any
}

// fakeUpstream is an OpenAI-compatible endpoint whose handler is chosen per
// test. It counts requests and records the latest one.
type fakeUpstream struct {
	requests atomic.Int32
	mu       sync.Mutex
	last     capturedRequest
	respond  func(w http.ResponseWriter, r *http.Request, n int)
}

func (u *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := int(u.requests.Add(1))

	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	u.mu.Lock()
	u.last = capturedRequest{
		method:        r.Method,
		path:          r.URL.Path,
		authorization: r.Header.Get("Authorization"),
		contentType:   r.Header.Get("Content-Type"),
		body:          body,
	}
	u.mu.Unlock()

	u.respond(w, r, n)
}

func (u *fakeUpstream) lastRequest() capturedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.last
}

func writeCompletion(w http.ResponseWriter, content, reasoning string, promptTokens, completionTokens int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":    "chatcmpl-test",
		"model": testModel,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":              "assistant",
				"content":           content,
				"reasoning_content": reasoning,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	})
}

type testClient struct {
	client   *llmanalyzer.Client
	upstream *fakeUpstream
	reader   *sdkmetric.ManualReader
}

func newTestClient(t *testing.T, timeout time.Duration, respond func(w http.ResponseWriter, r *http.Request, n int)) testClient {
	t.Helper()

	upstream := &fakeUpstream{respond: respond}
	srv := httptest.NewTLSServer(upstream)
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(pool))
	require.NoError(t, err)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	client, err := llmanalyzer.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), provider, policy, llmanalyzer.Config{
		BaseURL:   srv.URL + "/v1",
		APIKey:    testAPIKey,
		Model:     testModel,
		Timeout:   timeout,
		MaxTokens: 0,
	})
	require.NoError(t, err)

	return testClient{client: client, upstream: upstream, reader: reader}
}

func (tc testClient) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, tc.reader.Collect(t.Context(), &data))
	return data
}

// counterValue sums the data points of the named int64 counter whose
// attributes include every want pair. Unknown instruments yield zero.
func counterValue(t *testing.T, data metricdata.ResourceMetrics, name string, want ...attribute.KeyValue) int64 {
	t.Helper()
	var total int64
	for _, scope := range data.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != name {
				continue
			}
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			require.True(t, ok, "%s must be an int64 counter", name)
			for _, dp := range sum.DataPoints {
				if hasAttrs(dp.Attributes, want) {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// histogramCount returns the sample count of the named float64 histogram
// across data points whose attributes include every want pair.
func histogramCount(t *testing.T, data metricdata.ResourceMetrics, name string, want ...attribute.KeyValue) uint64 {
	t.Helper()
	var total uint64
	for _, scope := range data.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != name {
				continue
			}
			hist, ok := instrument.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "%s must be a float64 histogram", name)
			for _, dp := range hist.DataPoints {
				if hasAttrs(dp.Attributes, want) {
					total += dp.Count
				}
			}
		}
	}
	return total
}

func hasAttrs(set attribute.Set, want []attribute.KeyValue) bool {
	for _, kv := range want {
		got, ok := set.Value(kv.Key)
		if !ok || got != kv.Value {
			return false
		}
	}
	return true
}

func TestClient_CompleteHappyPath(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeCompletion(w, "  "+cleanVerdict+"\n", "", 12, 7)
	})

	messages := llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "hello", ToolCalls: nil, ToolOutcome: ""})
	completion, err := tc.client.Complete(t.Context(), testInfo, messages)
	require.NoError(t, err)
	require.Equal(t, llmanalyzer.Completion{
		Content:          cleanVerdict,
		PromptTokens:     12,
		CompletionTokens: 7,
		Model:            testModel,
		Attempts:         1,
	}, completion)
	require.Equal(t, int32(1), tc.upstream.requests.Load())

	req := tc.upstream.lastRequest()
	require.Equal(t, http.MethodPost, req.method)
	require.Equal(t, "/v1/chat/completions", req.path)
	require.Equal(t, "Bearer "+testAPIKey, req.authorization)
	require.Equal(t, "application/json", req.contentType)
	require.Equal(t, testModel, req.body["model"])
	require.InDelta(t, 0, req.body["temperature"], 0)
	require.InDelta(t, llmanalyzer.DefaultMaxTokens, req.body["max_tokens"], 0)
	require.Equal(t, map[string]any{"enable_thinking": false}, req.body["chat_template_kwargs"])
	require.Equal(t, []any{
		map[string]any{"role": "system", "content": llmanalyzer.SystemPrompt},
		map[string]any{"role": "user", "content": messages[1].Content},
	}, req.body["messages"])

	data := tc.collect(t)
	dims := []attribute.KeyValue{
		attr.OrganizationID(testInfo.OrgID),
		attr.OrganizationSlug(testInfo.OrgSlug),
		attr.RiskScanMode(testInfo.ScanMode),
		attr.RiskLLMModel(testModel),
	}
	require.Equal(t, int64(1), counterValue(t, data, "risk.llm.requests", append(dims, attr.Outcome(o11y.OutcomeSuccess))...))
	require.Equal(t, uint64(1), histogramCount(t, data, "risk.llm.duration", append(dims, attr.Outcome(o11y.OutcomeSuccess))...))
	require.Equal(t, int64(12), counterValue(t, data, "risk.llm.tokens", append(dims, attr.RiskLLMTokenKind(llmanalyzer.TokenKindInput))...))
	require.Equal(t, int64(7), counterValue(t, data, "risk.llm.tokens", append(dims, attr.RiskLLMTokenKind(llmanalyzer.TokenKindOutput))...))
	require.Equal(t, int64(0), counterValue(t, data, "risk.llm.retries"))
}

func TestClient_ReasoningContentFallback(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeCompletion(w, "", "\n"+cleanVerdict, 3, 4)
	})

	completion, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	require.NoError(t, err)
	require.Equal(t, llmanalyzer.Completion{
		Content:          cleanVerdict,
		PromptTokens:     3,
		CompletionTokens: 4,
		Model:            testModel,
		Attempts:         1,
	}, completion)
}

func TestClient_RetriesServerErrorThenSucceeds(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, n int) {
		if n == 1 {
			http.Error(w, `{"error": "overloaded"}`, http.StatusInternalServerError)
			return
		}
		writeCompletion(w, cleanVerdict, "", 1, 1)
	})

	completion, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	require.NoError(t, err)
	require.Equal(t, 2, completion.Attempts)
	require.Equal(t, int32(2), tc.upstream.requests.Load())

	data := tc.collect(t)
	require.Equal(t, int64(1), counterValue(t, data, "risk.llm.retries", attr.OrganizationID(testInfo.OrgID), attr.RiskScanMode(testInfo.ScanMode)))
	require.Equal(t, int64(1), counterValue(t, data, "risk.llm.requests", attr.Outcome(o11y.OutcomeSuccess)))
}

func TestClient_RetriesRateLimitedAndClampsRetryAfter(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, n int) {
		if n <= 2 {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		writeCompletion(w, cleanVerdict, "", 1, 1)
	})

	start := time.Now()
	completion, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	require.NoError(t, err)
	require.Equal(t, 3, completion.Attempts)
	require.Less(t, time.Since(start), 4*time.Second, "Retry-After must be clamped to the backoff cap")

	data := tc.collect(t)
	require.Equal(t, int64(2), counterValue(t, data, "risk.llm.retries"))
}

func TestClient_RetryAfterZeroStillBacksOff(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, n int) {
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		writeCompletion(w, cleanVerdict, "", 1, 1)
	})

	start := time.Now()
	completion, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	require.NoError(t, err)
	require.Equal(t, 3, completion.Attempts)
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "Retry-After: 0 must still wait the minimum backoff before each retry")
}

func TestClient_RateLimitedExhausted(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 10*time.Second, func(w http.ResponseWriter, _ *http.Request, _ int) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	})

	completion, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	upstream, ok := errors.AsType[*llmanalyzer.UpstreamError](err)
	require.True(t, ok, "expected *UpstreamError, got %v", err)
	require.Equal(t, http.StatusTooManyRequests, upstream.Status)
	require.Contains(t, upstream.Body, "slow down")
	require.Equal(t, 4, completion.Attempts)
	require.Equal(t, int32(4), tc.upstream.requests.Load())

	data := tc.collect(t)
	require.Equal(t, int64(1), counterValue(t, data, "risk.llm.requests", attr.Outcome(llmanalyzer.OutcomeRateLimited)))
	require.Equal(t, int64(3), counterValue(t, data, "risk.llm.retries"))
	require.Equal(t, int64(0), counterValue(t, data, "risk.llm.tokens"))
}

func TestClient_ServerErrorExhausted(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 10*time.Second, func(w http.ResponseWriter, _ *http.Request, _ int) {
		http.Error(w, "boom", http.StatusBadGateway)
	})

	completion, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	upstream, ok := errors.AsType[*llmanalyzer.UpstreamError](err)
	require.True(t, ok, "expected *UpstreamError, got %v", err)
	require.Equal(t, http.StatusBadGateway, upstream.Status)
	require.Equal(t, 4, completion.Attempts)
	require.Equal(t, int32(4), tc.upstream.requests.Load())

	data := tc.collect(t)
	require.Equal(t, int64(1), counterValue(t, data, "risk.llm.requests", attr.Outcome(o11y.OutcomeFailure)))
	require.Equal(t, int64(3), counterValue(t, data, "risk.llm.retries"))
}

func TestClient_BadRequestNotRetried(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, _ int) {
		http.Error(w, `{"error": {"message": "unknown model"}}`, http.StatusBadRequest)
	})

	completion, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	upstream, ok := errors.AsType[*llmanalyzer.UpstreamError](err)
	require.True(t, ok, "expected *UpstreamError, got %v", err)
	require.Equal(t, http.StatusBadRequest, upstream.Status)
	require.Contains(t, upstream.Body, "unknown model")
	require.Equal(t, 1, completion.Attempts)
	require.Equal(t, int32(1), tc.upstream.requests.Load())

	data := tc.collect(t)
	require.Equal(t, int64(1), counterValue(t, data, "risk.llm.requests", attr.Outcome(o11y.OutcomeFailure)))
	require.Equal(t, int64(0), counterValue(t, data, "risk.llm.retries"))
}

func TestClient_TimeoutIsNotRetried(t *testing.T) {
	t.Parallel()

	released := make(chan struct{})
	tc := newTestClient(t, 200*time.Millisecond, func(w http.ResponseWriter, r *http.Request, _ int) {
		// Hold the response until the client gives up and closes the
		// connection, which cancels the request context.
		<-r.Context().Done()
		close(released)
	})

	start := time.Now()
	completion, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	require.ErrorIs(t, err, llmanalyzer.ErrTimeout)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 3*time.Second)
	require.Equal(t, 1, completion.Attempts)

	<-released
	require.Equal(t, int32(1), tc.upstream.requests.Load())

	data := tc.collect(t)
	require.Equal(t, int64(1), counterValue(t, data, "risk.llm.requests", attr.Outcome(o11y.OutcomeTimeout)))
	require.Equal(t, uint64(1), histogramCount(t, data, "risk.llm.duration", attr.Outcome(o11y.OutcomeTimeout)))
	require.Equal(t, int64(0), counterValue(t, data, "risk.llm.retries"))
}

func TestClient_EmptyCompletion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "no choices", body: `{"model": "m", "choices": [], "usage": {"prompt_tokens": 1, "completion_tokens": 0}}`},
		{name: "blank content and reasoning", body: `{"model": "m", "choices": [{"message": {"content": "  ", "reasoning_content": ""}}]}`},
		{name: "null content", body: `{"model": "m", "choices": [{"message": {"content": null}}]}`},
		{name: "empty body", body: ""},
		{name: "whitespace body", body: " \n\t"},
	}
	for _, tc := range cases {
		body := tc.body
		client := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, _ int) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, body)
		})
		_, err := client.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
		require.ErrorIs(t, err, llmanalyzer.ErrEmptyCompletion, tc.name)
	}
}

func TestClient_DoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, r *http.Request, _ int) {
		if r.URL.Path == "/v1/chat/completions" {
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
			return
		}
		writeCompletion(w, cleanVerdict, "", 1, 1)
	})

	_, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	upstream, ok := errors.AsType[*llmanalyzer.UpstreamError](err)
	require.True(t, ok, "expected *UpstreamError, got %v", err)
	require.Equal(t, http.StatusTemporaryRedirect, upstream.Status)
	require.Equal(t, int32(1), tc.upstream.requests.Load(), "the redirect target must never be requested")
	require.Equal(t, "/v1/chat/completions", tc.upstream.lastRequest().path)
}

func TestClient_MalformedResponse(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, _ int) {
		_, _ = fmt.Fprint(w, "<html>not json</html>")
	})

	_, err := tc.client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	require.Error(t, err)
	require.ErrorContains(t, err, "decode risk llm response")
	require.NotErrorIs(t, err, llmanalyzer.ErrTimeout)
}

func TestClient_RecordParseFailure(t *testing.T) {
	t.Parallel()

	tc := newTestClient(t, 5*time.Second, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeCompletion(w, "not a verdict", "", 1, 1)
	})

	tc.client.RecordParseFailure(t.Context(), testInfo)
	tc.client.RecordParseFailure(t.Context(), testInfo)

	data := tc.collect(t)
	require.Equal(t, int64(2), counterValue(t, data, "risk.llm.parse_failures",
		attr.OrganizationID(testInfo.OrgID),
		attr.OrganizationSlug(testInfo.OrgSlug),
		attr.RiskScanMode(testInfo.ScanMode),
		attr.RiskLLMModel(testModel),
	))
}

func TestClient_BaseURLTrailingSlash(t *testing.T) {
	t.Parallel()

	upstream := &fakeUpstream{respond: func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeCompletion(w, cleanVerdict, "", 1, 1)
	}}
	srv := httptest.NewTLSServer(upstream)
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(pool))
	require.NoError(t, err)

	client, err := llmanalyzer.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), policy, llmanalyzer.Config{
		BaseURL:   srv.URL + "/v1/",
		APIKey:    testAPIKey,
		Model:     testModel,
		Timeout:   0,
		MaxTokens: 0,
	})
	require.NoError(t, err)

	_, err = client.Complete(t.Context(), testInfo, llmanalyzer.BuildMessages(llmanalyzer.PromptInput{Content: "x", ToolCalls: nil, ToolOutcome: ""}))
	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions", upstream.lastRequest().path)
}

func TestNewClient_RejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	_, err = llmanalyzer.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), policy, llmanalyzer.Config{
		BaseURL:   "http://model.example.com/v1",
		APIKey:    testAPIKey,
		Model:     testModel,
		Timeout:   0,
		MaxTokens: 0,
	})
	require.ErrorContains(t, err, "scheme must be https")

	_, err = llmanalyzer.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, llmanalyzer.Config{
		BaseURL:   "https://model.example.com/v1",
		APIKey:    testAPIKey,
		Model:     testModel,
		Timeout:   0,
		MaxTokens: 0,
	})
	require.ErrorContains(t, err, "guardian policy is required")
}
