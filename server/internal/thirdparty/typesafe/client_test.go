package typesafe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const secretQuery = "super secret query text"

func sampleRequest() Request {
	return Request{
		Model: DefaultModel,
		State: json.RawMessage(`{"query":"` + secretQuery + `"}`),
		Questions: map[string]Question{
			"kind": {
				Type:         "choice",
				Instructions: "Which kind of thing is the user asking for?",
				Criteria: map[string]string{
					"page":   "Page: Settings",
					"server": "MCP server: Billing",
				},
			},
			"ready": {
				Type:         "noul",
				Instructions: "Is the query complete enough to act on?",
				Criteria:     map[string]string{"true": "yes", "false": "no"},
			},
		},
	}
}

func newTestClient(t *testing.T, endpoint string, opts ...Option) (*Client, *tracetest.SpanRecorder) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })

	opts = append([]Option{WithEndpoint(endpoint), WithTracerProvider(provider)}, opts...)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	client := NewClient(policy.Client(), testenv.NewLogger(t), opts...)
	return client, recorder
}

func TestAskHappyPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected Authorization Bearer test-key, got %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", got)
		}

		var body Request
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if body.Model != DefaultModel {
			t.Errorf("expected model %q, got %q", DefaultModel, body.Model)
		}
		if got := body.Questions["ready"].Type; got != "noul" {
			t.Errorf("expected ready question type noul, got %q", got)
		}
		if !strings.Contains(string(body.State), secretQuery) {
			t.Errorf("expected state to carry the query, got %s", string(body.State))
		}

		_, _ = io.WriteString(w, `{
			"id": "gen-01",
			"provider": "TypeSafe",
			"model": "jev-2026-01",
			"answers": {
				"kind": {"type": "choice", "choice": "page", "probabilities": {"page": 0.8, "server": 0.2}},
				"ready": {"type": "noul", "noul": 0.35}
			},
			"usage": {"input_tokens": 120, "output_tokens": 7, "cost": 0.00042}
		}`)
	}))
	t.Cleanup(server.Close)

	client, recorder := newTestClient(t, server.URL)
	result, err := client.Ask(t.Context(), "test-key", sampleRequest())
	require.NoError(t, err)
	require.Equal(t, "jev-2026-01", result.Response.Model)
	require.Equal(t, Usage{InputTokens: 120, OutputTokens: 7}, result.Response.Usage)
	require.Positive(t, result.Latency)

	kind := result.Response.Answers["kind"]
	require.Equal(t, "choice", kind.Type)
	require.NotNil(t, kind.Choice)
	require.Equal(t, "page", *kind.Choice)
	require.InDelta(t, 0.8, kind.Probabilities["page"], 1e-9)
	require.InDelta(t, 0.2, kind.Probabilities["server"], 1e-9)
	require.Nil(t, kind.Noul)

	ready := result.Response.Answers["ready"]
	require.Equal(t, "noul", ready.Type)
	require.NotNil(t, ready.Noul)
	require.InDelta(t, 0.35, *ready.Noul, 1e-9)
	require.Nil(t, ready.Choice)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	attrs := spans[0].Attributes()
	require.Contains(t, attrs, attribute.Int("gen_ai.usage.input_tokens", 120))
	require.Contains(t, attrs, attribute.Int("gen_ai.usage.output_tokens", 7))
	require.Contains(t, attrs, attribute.Int("gram.typesafe.question_count", 2))
	require.Contains(t, attrs, attribute.String("gen_ai.request.model", DefaultModel))
	for _, kv := range attrs {
		require.NotContains(t, kv.Value.Emit(), secretQuery, "query text must never reach span attributes")
		require.NotContains(t, kv.Value.Emit(), "Settings", "candidate titles must never reach span attributes")
	}
	var sawLatency bool
	for _, kv := range attrs {
		if kv.Key == "gram.typesafe.latency_ms" {
			sawLatency = true
		}
	}
	require.True(t, sawLatency, "expected latency attribute on span")
}

func TestAskUpstreamStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"nope"}`)
			}))
			t.Cleanup(server.Close)

			client, _ := newTestClient(t, server.URL)
			result, err := client.Ask(t.Context(), "test-key", sampleRequest())
			require.Nil(t, result)
			require.ErrorIs(t, err, ErrUpstreamStatus)
			require.NotErrorIs(t, err, ErrTransport)
			require.NotErrorIs(t, err, ErrDecode)
			require.Contains(t, err.Error(), http.StatusText(status))
		})
	}
}

func TestAskMalformedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"model": "jev-latest", "answers": {`)
	}))
	t.Cleanup(server.Close)

	client, _ := newTestClient(t, server.URL)
	result, err := client.Ask(t.Context(), "test-key", sampleRequest())
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrDecode)
	require.NotErrorIs(t, err, ErrUpstreamStatus)
}

func TestAskTimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the body so the server notices the client hanging up; it only
		// watches the connection once the request body has been consumed.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	// Cleanups run last-in first-out: release the handler before Close waits on it.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	client, _ := newTestClient(t, server.URL, WithTimeout(50*time.Millisecond))
	started := time.Now()
	result, err := client.Ask(t.Context(), "test-key", sampleRequest())
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrTransport)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 2*time.Second)
}

func TestAskMissingAPIKey(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"model":"jev-latest","answers":{},"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	t.Cleanup(server.Close)

	client, _ := newTestClient(t, server.URL)
	result, err := client.Ask(t.Context(), "", sampleRequest())
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrMissingAPIKey)
	require.NotErrorIs(t, err, ErrTransport)
	require.Equal(t, int32(0), requests.Load(), "no request must be made without an API key")
}
