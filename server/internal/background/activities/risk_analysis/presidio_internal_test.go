package risk_analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func TestChunkTextIndexes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    int
		size int
		want []indexRange
	}{
		{name: "empty", n: 0, size: 50, want: nil},
		{name: "smaller than size", n: 7, size: 50, want: []indexRange{{0, 7}}},
		{name: "exact multiple", n: 100, size: 50, want: []indexRange{{0, 50}, {50, 100}}},
		{name: "uneven last batch", n: 125, size: 50, want: []indexRange{{0, 50}, {50, 100}, {100, 125}}},
		{name: "size one", n: 3, size: 1, want: []indexRange{{0, 1}, {1, 2}, {2, 3}}},
		{name: "single item", n: 1, size: 50, want: []indexRange{{0, 1}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := chunkTextIndexes(tc.n, tc.size)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestStubPIIScannerReturnsEmptyResults(t *testing.T) {
	t.Parallel()

	results, err := (&StubPIIScanner{}).AnalyzeBatch(t.Context(), []string{"one", "two"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, findings := range results {
		assert.Empty(t, findings)
	}
}

func TestPresidioAnalyzeBatchSplitsPoisonedBatch(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests [][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want %s", r.Method, http.MethodPost)
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/analyze" {
			t.Errorf("path = %s, want /analyze", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		var req presidioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		mu.Lock()
		requests = append(requests, slices.Clone(req.Text))
		mu.Unlock()

		if slices.Contains(req.Text, "poison") {
			http.Error(w, "poison text", http.StatusInternalServerError)
			return
		}

		results := make([][]presidioResult, len(req.Text))
		for i, text := range req.Text {
			start := strings.Index(text, "alice@example.com")
			if start < 0 {
				continue
			}
			results[i] = []presidioResult{{
				EntityType: "EMAIL_ADDRESS",
				Start:      start,
				End:        start + len("alice@example.com"),
				Score:      1,
			}}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewPresidioClientWithConcurrency(
		srv.URL,
		otel.GetTracerProvider(),
		otel.GetMeterProvider(),
		testLogger(t),
		1,
	)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"clean",
		"contact alice@example.com",
		"poison",
		"backup alice@example.com",
	}, nil, nil)
	// AnalyzeBatch surfaces the first observed Presidio HTTP failure
	// alongside partial results; callers must consume results regardless.
	require.Error(t, err)
	require.ErrorContains(t, err, "presidio returned status 500")
	require.Len(t, results, 4)

	assert.Empty(t, results[0])
	require.Len(t, results[1], 1)
	assert.Equal(t, "alice@example.com", results[1][0].Match)
	assert.Empty(t, results[2])
	require.Len(t, results[3], 1)
	assert.Equal(t, "alice@example.com", results[3][0].Match)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, [][]string{
		{"clean", "contact alice@example.com", "poison", "backup alice@example.com"},
		{"clean", "contact alice@example.com"},
		{"poison", "backup alice@example.com"},
		{"poison"},
		{"backup alice@example.com"},
	}, requests)
}

func TestPresidioAnalyzeBatchSplitsUntilSingleTexts(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests [][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req presidioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		mu.Lock()
		requests = append(requests, slices.Clone(req.Text))
		mu.Unlock()

		http.Error(w, "presidio down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := NewPresidioClientWithConcurrency(
		srv.URL,
		otel.GetTracerProvider(),
		otel.GetMeterProvider(),
		testLogger(t),
		1,
	)

	results, err := client.AnalyzeBatch(t.Context(), []string{
		"one",
		"two",
		"three",
		"four",
	}, nil, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "presidio returned status 503")
	require.Len(t, results, 4)
	for _, findings := range results {
		assert.Empty(t, findings)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, [][]string{
		{"one", "two", "three", "four"},
		{"one", "two"},
		{"three", "four"},
		{"one"},
		{"two"},
		{"three"},
		{"four"},
	}, requests)
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), nil))
}

func TestPresidioAnalyzeTimesOutPerRequest(t *testing.T) {
	t.Parallel()

	// Server blocks until the test's deadline ctx fires so the only way
	// out is the client's per-request timeout. Without a per-request
	// timeout the test would hang for the full t.Context() budget.
	released := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	// LIFO cleanup: release in-flight handlers before srv.Close blocks on
	// them. Keep-alive connection reuse means a client-side reqCtx timeout
	// does not always propagate to r.Context() Done.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })

	client := NewPresidioClientWithConcurrency(
		srv.URL,
		otel.GetTracerProvider(),
		otel.GetMeterProvider(),
		testLogger(t),
		1,
	)
	client.requestTimeout = 100 * time.Millisecond

	start := time.Now()
	results, err := client.AnalyzeBatch(t.Context(), []string{"hang"}, nil, nil)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Len(t, results, 1)
	assert.Empty(t, results[0])
	// Single text bisects to depth 0 immediately, so total time is one
	// request timeout + bounded retry sleep. Generous upper bound to
	// avoid flakes under CI load.
	assert.Less(t, elapsed, 5*time.Second, "per-request timeout did not bound elapsed time: %s", elapsed)
}

func TestIsCancelErrClassifiesContextErrors(t *testing.T) {
	t.Parallel()

	assert.True(t, isCancelErr(context.Canceled))
	assert.True(t, isCancelErr(context.DeadlineExceeded))
	assert.True(t, isCancelErr(fmt.Errorf("wrapped: %w", context.Canceled)))
	assert.True(t, isCancelErr(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)))
	assert.False(t, isCancelErr(nil))
	assert.False(t, isCancelErr(errors.New("presidio returned status 500")))
}

func TestMergeFirstErrPrefersNonCancel(t *testing.T) {
	t.Parallel()

	boom := errors.New("presidio returned status 500")
	wrappedCancel := fmt.Errorf("http request: %w", context.Canceled)

	require.NoError(t, mergeFirstErr(nil, nil))
	assert.Equal(t, boom, mergeFirstErr(nil, boom))
	assert.Equal(t, boom, mergeFirstErr(boom, nil))
	// First-write wins when both are equally diagnostic.
	assert.Equal(t, boom, mergeFirstErr(boom, errors.New("other 500")))
	// Cancel-then-non-cancel: prefer the non-cancel cause.
	assert.Equal(t, boom, mergeFirstErr(context.Canceled, boom))
	assert.Equal(t, boom, mergeFirstErr(wrappedCancel, boom))
	assert.Equal(t, boom, mergeFirstErr(context.DeadlineExceeded, boom))
	// Non-cancel-then-cancel: keep the non-cancel cause.
	assert.Equal(t, boom, mergeFirstErr(boom, context.Canceled))
	assert.Equal(t, boom, mergeFirstErr(boom, wrappedCancel))
	// Two cancel errors: first wins (no useful diagnostic to swap to).
	assert.Equal(t, context.Canceled, mergeFirstErr(context.Canceled, context.DeadlineExceeded))
}
