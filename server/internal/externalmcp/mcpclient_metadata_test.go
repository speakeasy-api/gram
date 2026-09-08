package externalmcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// Reuse the wire-level fixture but allow blocking and arbitrarily paginated listings.
func newMetadataClient(t *testing.T, fixture *parameterServer, list func(parameterRequest) any) func() *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request", http.StatusBadRequest)
			return
		}
		var req parameterRequest
		if json.Unmarshal(body, &req) == nil && req.Method == "tools/list" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": list(req)})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		fixture.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return func() *Client {
		c, err := NewClient(t.Context(), testenv.NewLogger(t), policy, server.URL, types.TransportTypeStreamableHTTP, &ClientOptions{MetadataScope: t.Name()})
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
}

func metadataListing(schema string) any {
	return map[string]any{"tools": []any{map[string]any{"name": "lookup", "inputSchema": json.RawMessage(schema)}}}
}

func TestClientMetadataRecoveryUpdatesReusedClient(t *testing.T) {
	t.Parallel()
	s := &parameterServer{schema: annotatedSchema}
	c := newParameterClient(t, s, &ClientOptions{MetadataScope: t.Name()})()
	_, err := c.ListTools(t.Context())
	require.NoError(t, err)
	s.mu.Lock()
	s.schema = plainSchema
	s.wantParameters = make(http.Header)
	s.mu.Unlock()
	args := json.RawMessage(`{"owner":"value"}`)
	_, err = c.CallTool(t.Context(), "lookup", args)
	require.NoError(t, err)
	_, err = c.CallTool(t.Context(), "lookup", args)
	require.NoError(t, err)
	require.Len(t, s.matching("tools/call"), 3, "only the first call should need a replay")
	require.Len(t, s.matching("tools/list"), 2)
	require.JSONEq(t, plainSchema, string(c.discovered["lookup"]))
}

func TestClientMetadataOlderDiscoveryCannotOverwriteRecovery(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	var lists atomic.Int32
	s := &parameterServer{schema: plainSchema, wantParameters: make(http.Header)}
	makeClient := newMetadataClient(t, s, func(parameterRequest) any {
		if lists.Add(1) == 1 {
			close(started)
			<-release
			return metadataListing(annotatedSchema)
		}
		return metadataListing(plainSchema)
	})
	old := makeClient()
	done := make(chan error, 1)
	go func() {
		_, err := old.ListTools(t.Context())
		done <- err
	}()
	<-started
	// Always unblock the old request before test cleanup closes its session.
	releaseOld := sync.OnceFunc(func() { close(release) })
	defer releaseOld()
	fresh := makeClient()
	_, err := fresh.CallTool(t.Context(), "lookup", json.RawMessage(`{"owner":"value"}`), json.RawMessage(annotatedSchema))
	require.NoError(t, err)
	releaseOld()
	require.NoError(t, <-done)
	schema, ok := cachedToolSchema(fresh.metadataKey, "lookup")
	require.True(t, ok)
	require.JSONEq(t, plainSchema, string(schema))
}

func TestClientMetadataInvalidDiscoveryClearsCachedTarget(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{"absent", "null", `{"properties":{"owner":{"type":"string","x-mcp-header":"bad header"}}}`} {
		t.Run(schema, func(t *testing.T) {
			t.Parallel()
			makeClient := newMetadataClient(t, &parameterServer{}, func(parameterRequest) any {
				if schema == "absent" {
					return map[string]any{"tools": []any{}}
				}
				return metadataListing(schema)
			})
			c := makeClient()
			sharedToolMetadata.put(c.metadataKey, "lookup", json.RawMessage(annotatedSchema))
			c.discovered["lookup"] = json.RawMessage(annotatedSchema)
			err := c.discoverTool(t.Context(), "lookup")
			require.Error(t, err)
			_, ok := cachedToolSchema(c.metadataKey, "lookup")
			require.False(t, ok)
			require.NotContains(t, c.discovered, "lookup")
		})
	}
}

func TestClientMetadataListToolsAggregateBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema, want string
		perPage, pages     int
	}{
		{"tool-count", `{}`, "exceeds tool limit", maxListedTools / 2, 3},
		{"empty-pages", `{}`, "exceeds page limit", 0, maxListedPages},
		{"schema-bytes", `{"description":"` + strings.Repeat("x", 32<<10) + `"}`, "exceeds schema byte limit", 64, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var pages atomic.Int32
			makeClient := newMetadataClient(t, &parameterServer{}, func(req parameterRequest) any {
				page := pages.Add(1)
				tools := make([]any, tc.perPage)
				for i := range tools {
					tools[i] = map[string]any{"name": fmt.Sprintf("tool-%d-%d", page, i), "inputSchema": json.RawMessage(tc.schema)}
				}
				return map[string]any{"tools": tools, "nextCursor": fmt.Sprint(page)}
			})
			c := makeClient()
			tools, err := c.ListTools(t.Context())
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, tools)
			require.EqualValues(t, tc.pages, pages.Load())
			require.Empty(t, c.discovered, "failed partial listings must not be retained")
			_, ok := cachedToolSchema(c.metadataKey, "tool-1-0")
			require.False(t, ok)
		})
	}
}

func TestClientMetadataCachedListingCannotOverwriteRecovery(t *testing.T) {
	t.Parallel()
	s := &parameterServer{schema: annotatedSchema}
	makeClient := newParameterClient(t, s, &ClientOptions{MetadataScope: t.Name()})
	old := makeClient()
	_, err := old.ListTools(t.Context())
	require.NoError(t, err)
	s.mu.Lock()
	s.schema = plainSchema
	s.wantParameters = make(http.Header)
	s.mu.Unlock()
	fresh := makeClient()
	_, err = fresh.CallTool(t.Context(), "lookup", json.RawMessage(`{"owner":"value"}`))
	require.NoError(t, err)
	_, err = old.ListTools(t.Context())
	require.NoError(t, err)
	require.Len(t, s.matching("tools/list"), 2, "old client should reuse the SDK listing cache")
	schema, ok := cachedToolSchema(fresh.metadataKey, "lookup")
	require.True(t, ok)
	require.JSONEq(t, plainSchema, string(schema))
}
