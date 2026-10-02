package remotemcp_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestProxyManagerLabelsListResultsCallerVarying covers every interceptor
// combination ProxyManager attaches to a tools/list or resources/list chain:
// none on a public server, the mcp:connect filter on a private one, and the
// session selection filter on either. The filters leave labelling to the
// proxy, so this is where a regression in any of them would surface.
func TestProxyManagerLabelsListResultsCallerVarying(t *testing.T) {
	t.Parallel()

	const (
		toolsListRequest      = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
		resourcesListRequest  = `{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{}}`
		toolsListUpstream     = `{"jsonrpc":"2.0","id":2,"result":{"ttlMs":60000,"cacheScope":"public","tools":[{"name":"a","inputSchema":{}},{"name":"b","inputSchema":{}}]}}`
		resourcesListUpstream = `{"jsonrpc":"2.0","id":3,"result":{"ttlMs":60000,"cacheScope":"public","resources":[{"name":"a","uri":"file:///a"}]}}`
	)

	cases := []struct {
		name       string
		visibility string
		selection  *toolfilter.SessionSelection
	}{
		{name: "public", visibility: mcpservers.VisibilityPublic, selection: nil},
		{name: "public with session selection", visibility: mcpservers.VisibilityPublic, selection: selectionOf(t, "a")},
		{name: "private", visibility: mcpservers.VisibilityPrivate, selection: nil},
		{name: "private with session selection", visibility: mcpservers.VisibilityPrivate, selection: selectionOf(t, "a")},
	}
	for _, tc := range cases {
		for _, list := range []struct {
			method   string
			request  string
			upstream string
		}{
			{method: "tools/list", request: toolsListRequest, upstream: toolsListUpstream},
			{method: "resources/list", request: resourcesListRequest, upstream: resourcesListUpstream},
		} {
			t.Run(tc.name+" "+list.method, func(t *testing.T) {
				t.Parallel()

				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, list.upstream)
				}))
				t.Cleanup(upstream.Close)

				logger := testenv.NewLogger(t)
				tracerProvider := testenv.NewTracerProvider(t)
				policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
				require.NoError(t, err)
				manager := remotemcp.NewProxyManager(
					logger,
					tracerProvider,
					testenv.NewMeterProvider(t),
					nil, policy, nil, nil, nil, nil, nil, nil, nil, nil,
					nil,
					nil,
				)
				p := manager.Build(
					logger,
					&remotemcprepo.RemoteMcpServer{ID: uuid.New(), Url: upstream.URL},
					uuid.NewString(),
					nil,
					tc.visibility,
					"organization-id",
					"project-id",
					"",
					"",
					tc.selection,
				)

				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(list.request))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				rr := httptest.NewRecorder()
				require.NoError(t, p.Post(rr, req))

				var envelope struct {
					Result map[string]json.RawMessage `json:"result"`
				}
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &envelope), rr.Body.String())
				require.JSONEq(t, `"private"`, string(envelope.Result["cacheScope"]),
					"a proxied list result must never fall back to the public cache default")
				require.JSONEq(t, `0`, string(envelope.Result["ttlMs"]),
					"a proxied list result must not inherit an upstream ttl")
				if tc.selection != nil && list.method == "tools/list" {
					require.JSONEq(t, `[{"name":"a","inputSchema":{}}]`, string(envelope.Result["tools"]),
						"the session selection filter must have run")
				}
			})
		}
	}
}
