package risk_analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

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

	client := NewPresidioClientWithWorkers(
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
	require.NoError(t, err)
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

	client := NewPresidioClientWithWorkers(
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
	require.NoError(t, err)
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

func TestPresidioAnalyzeBatchSkipsOversizeText(t *testing.T) {
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

	var logBuf bytes.Buffer
	var logMu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&syncWriter{w: &logBuf, mu: &logMu}, &slog.HandlerOptions{Level: slog.LevelWarn}))

	client := NewPresidioClientWithWorkers(srv.URL, otel.GetTracerProvider(), otel.GetMeterProvider(), logger, 1)

	oversize := strings.Repeat("a", presidioMaxTextBytes+1)
	results, err := client.AnalyzeBatch(t.Context(), []string{
		"contact alice@example.com",
		oversize,
		"",
		"backup alice@example.com",
	}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 4)

	require.Len(t, results[0], 1)
	assert.Equal(t, "alice@example.com", results[0][0].Match)
	assert.Empty(t, results[1], "oversize text should produce no findings")
	assert.Empty(t, results[2], "empty text should produce no findings")
	require.Len(t, results[3], 1)
	assert.Equal(t, "alice@example.com", results[3][0].Match)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 1, "single batch with only the eligible texts")
	assert.Equal(t, []string{
		"contact alice@example.com",
		"backup alice@example.com",
	}, requests[0], "oversize and empty texts should not be sent to presidio")

	logMu.Lock()
	defer logMu.Unlock()
	logs := logBuf.String()
	assert.Contains(t, logs, "presidio analyze: text exceeds max size")
	assert.Contains(t, logs, "gram.risk.presidio.text_index=1")
}

func TestPresidioAnalyzeBatchAllEmptyOrOversizeSkipsRequest(t *testing.T) {
	t.Parallel()

	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	client := NewPresidioClientWithWorkers(srv.URL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t), 1)

	oversize := strings.Repeat("b", presidioMaxTextBytes+1)
	results, err := client.AnalyzeBatch(t.Context(), []string{"", oversize, ""}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)
	for _, r := range results {
		assert.Empty(t, r)
	}
	assert.False(t, called, "presidio should not be called when every text is empty or oversize")
}

type syncWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("write log buffer: %w", err)
	}
	return n, nil
}
