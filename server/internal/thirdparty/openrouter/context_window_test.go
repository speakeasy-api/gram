package openrouter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newTestResolver(t *testing.T, handler http.Handler) *ContextWindowResolver {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	logger := testenv.NewLogger(t)
	return &ContextWindowResolver{
		logger:     logger,
		httpClient: srv.Client(),
		cache:      cache.NewTypedObjectCache[mv.ModelContextWindow](logger, cache.NoopCache, cache.SuffixNone),
		baseURL:    srv.URL,
	}
}

func TestContextWindowResolver_FetchMinPicksSmallest(t *testing.T) {
	t.Parallel()

	var gotPath string
	r := newTestResolver(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"endpoints":[
			{"provider_name":"Anthropic","context_length":200000},
			{"provider_name":"Bedrock","context_length":150000},
			{"provider_name":"Vertex","context_length":180000}
		]}}`))
	}))

	got, err := r.fetchMin(t.Context(), "anthropic/claude-opus-4.6")
	require.NoError(t, err)
	require.Equal(t, 150000, got)
	require.Equal(t, "/v1/models/anthropic/claude-opus-4.6/endpoints", gotPath)
}

func TestContextWindowResolver_FetchMinIgnoresZeroOrNegative(t *testing.T) {
	t.Parallel()

	r := newTestResolver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"endpoints":[
			{"provider_name":"A","context_length":0},
			{"provider_name":"B","context_length":-1},
			{"provider_name":"C","context_length":128000}
		]}}`))
	}))

	got, err := r.fetchMin(t.Context(), "openai/gpt-5.4")
	require.NoError(t, err)
	require.Equal(t, 128000, got)
}

func TestContextWindowResolver_FetchMinErrorsOnNoEndpoints(t *testing.T) {
	t.Parallel()

	r := newTestResolver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"endpoints":[]}}`))
	}))

	_, err := r.fetchMin(t.Context(), "openai/gpt-5.4")
	require.Error(t, err)
}

func TestContextWindowResolver_FetchMinErrorsOnAllZero(t *testing.T) {
	t.Parallel()

	r := newTestResolver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"endpoints":[
			{"provider_name":"A","context_length":0},
			{"provider_name":"B","context_length":0}
		]}}`))
	}))

	_, err := r.fetchMin(t.Context(), "openai/gpt-5.4")
	require.Error(t, err)
}

func TestContextWindowResolver_FetchMinErrorsOnNon200(t *testing.T) {
	t.Parallel()

	r := newTestResolver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	_, err := r.fetchMin(t.Context(), "openai/gpt-5.4")
	require.Error(t, err)
}

func TestContextWindowResolver_FetchMinErrorsOnInvalidModelID(t *testing.T) {
	t.Parallel()

	r := newTestResolver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatalf("upstream must not be called for invalid id")
	}))

	for _, bad := range []string{"", "/", "noslash", "/missing-author", "missing-slug/"} {
		_, err := r.fetchMin(t.Context(), bad)
		require.Error(t, err, "id=%q", bad)
	}
}

func TestContextWindowResolver_ResolveStoresInCacheOnFetch(t *testing.T) {
	t.Parallel()

	calls := 0
	r := newTestResolver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":{"endpoints":[{"provider_name":"A","context_length":42000}]}}`))
	}))

	got, err := r.Resolve(t.Context(), "openai/gpt-5.4")
	require.NoError(t, err)
	require.Equal(t, 42000, got)
	require.Equal(t, 1, calls)
}
