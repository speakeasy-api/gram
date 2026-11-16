package openrouter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedClientListModelsPreservesSupportedPolicy(t *testing.T) {
	t.Parallel()
	ids := AllowedModels()
	require.NotEmpty(t, ids)
	supported := fmt.Sprintf(`{"id":%q,"context_length":200000,"pricing":{"prompt":"0.000003"},"future_field":9007199254740993}`, ids[0])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/models", r.URL.Path)
		assert.Empty(t, r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		_, _ = fmt.Fprintf(w, `{"data":[%s,{"id":"unsupported/model"}],"future_envelope":{"version":2}}`, supported)
	}))
	defer server.Close()
	client := newTestClientForServer(t, server)
	// Discovery must not resolve/provision a spending key or invoke capture,
	// billing, title generation, or telemetry strategies.
	client.keyResolver = nil
	client.provisioner = nil
	client.messageCaptureStrategy = nil
	client.usageTrackingStrategy = nil
	client.chatTitleGenerator = nil
	client.telemetryLogger = nil
	// Exercise the same interface boundary as chat.Service, not just Fetch.
	var completion CompletionClient = client
	catalogClient, ok := completion.(ModelCatalogClient)
	require.True(t, ok, "NewUnifiedClient must expose the model catalog through CompletionClient")
	catalog, err := catalogClient.ListModels(t.Context())
	require.NoError(t, err)
	var entries []json.RawMessage
	require.NoError(t, json.Unmarshal(catalog["data"], &entries))
	require.Len(t, entries, 1)
	require.Equal(t, supported, string(entries[0]), "preserve upstream metadata and numeric precision")
	require.JSONEq(t, `{"version":2}`, string(catalog["future_envelope"]))
}

func TestUnifiedClientListModelsFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "unavailable", status: http.StatusServiceUnavailable, body: "private upstream detail"},
		{name: "malformed", status: http.StatusOK, body: `{"data":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := newTestClientForServer(t, server)
			catalog, err := client.ListModels(t.Context())
			require.ErrorContains(t, err, "list supported models:")
			require.Nil(t, catalog, "never substitute a fabricated catalog")
			require.NotContains(t, err.Error(), "private upstream detail")
		})
	}
}
