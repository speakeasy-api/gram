package proxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

func TestProxy_Post_ToolsListFilterCanonicalizesDuplicateToolMembers(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":9,"result":{"tools":[{"name":"hidden","name":"allowed","inputSchema":{}}]}}`)
	}))
	t.Cleanup(upstream.Close)

	p := newProxyForTest(t, upstream.URL)
	p.ToolsListResponseInterceptors = []proxy.ToolsListResponseInterceptor{
		&mutatingToolsListResponseInterceptor{
			name:    "keep-all",
			toolsFn: func(tools []*mcp.Tool) []*mcp.Tool { return tools },
			err:     nil,
		},
	}

	rr, err := postJSON(t, p, strictToolsListRequest)
	require.NoError(t, err)
	require.NotContains(t, rr.Body.String(), "hidden")
	require.Contains(t, rr.Body.String(), `"name":"allowed"`)
	require.Equal(t, 1, strings.Count(rr.Body.String(), `"name"`),
		"the rewritten item must contain one canonical name for every parser")
}
