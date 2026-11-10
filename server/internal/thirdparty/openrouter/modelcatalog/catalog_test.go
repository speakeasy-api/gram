package modelcatalog_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/modelcatalog"
)

func TestFetchPreservesSupportedMetadata(t *testing.T) {
	t.Parallel()
	ids := openrouter.AllowedModels()
	first := fmt.Sprintf(`{"id":%q,"name":"Fixture model","context_length":200000,"top_provider":{"context_length":180000,"max_completion_tokens":12000},"pricing":{"prompt":"0.000003"},"architecture":{"input_modalities":["text","image"]},"future_field":{"precise_integer":9007199254740993}}`, ids[len(ids)-1])
	second := fmt.Sprintf(`{"id":%q,"context_length":64000}`, ids[0])
	body := `{"data":[` + first + `,{"id":"unsupported/model","context_length":999},` + second + `],"future_envelope":{"version":2}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/models", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)
		assert.Empty(t, r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	got, err := modelcatalog.Fetch(t.Context(), server.Client(), server.URL+"/api", openrouter.IsModelAllowed)
	require.NoError(t, err)
	require.JSONEq(t, `{"version":2}`, string(got["future_envelope"]))
	var entries []json.RawMessage
	require.NoError(t, json.Unmarshal(got["data"], &entries))
	require.Len(t, entries, 2)
	// Raw comparisons retain large numbers, nested metadata, and upstream order.
	require.Equal(t, first, string(entries[0]))
	require.Equal(t, second, string(entries[1]))
	require.NotContains(t, got, "object", "do not synthesize envelope metadata")
}

func TestFetchReturnsEverySupportedUpstreamEntry(t *testing.T) {
	t.Parallel()
	ids := openrouter.AllowedModels()
	entries := make([]map[string]string, 0, len(ids)+1)
	for _, id := range ids {
		entries = append(entries, map[string]string{"id": id})
	}
	entries = append(entries, map[string]string{"id": "unsupported/model"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": entries}))
	}))
	defer server.Close()
	got, err := modelcatalog.Fetch(t.Context(), server.Client(), server.URL, openrouter.IsModelAllowed)
	require.NoError(t, err)
	var models []struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(got["data"], &models))
	actual := make([]string, 0, len(models))
	for _, model := range models {
		actual = append(actual, model.ID)
	}
	require.Equal(t, ids, actual)
}

func TestFetchFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "upstream refusal", status: 503, body: "private upstream error body"},
		{name: "malformed JSON", status: 200, body: "not JSON"},
		{name: "missing data", status: 200, body: `{}`},
		{name: "null data", status: 200, body: `{"data":null}`},
		{name: "missing model id", status: 200, body: `{"data":[{}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, err := modelcatalog.Fetch(t.Context(), server.Client(), server.URL, openrouter.IsModelAllowed)
			require.Error(t, err)
			require.Nil(t, got)
			require.NotContains(t, err.Error(), "private upstream error body")
		})
	}
}

func TestFetchEmptySupportedSubset(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"unsupported/model"}]}`))
	}))
	defer server.Close()
	got, err := modelcatalog.Fetch(t.Context(), server.Client(), server.URL, openrouter.IsModelAllowed)
	require.NoError(t, err)
	require.Equal(t, "[]", string(got["data"]))
}
