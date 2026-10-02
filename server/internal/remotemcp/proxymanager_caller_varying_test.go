package remotemcp_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestProxyManagerLabelsListResultsCallerVarying covers the proxy
// configurations ProxyManager builds for a tools/list or resources/list
// chain. The filters leave labelling to the proxy, so this is where a
// regression in any of them would surface.
//
// Only an anonymous caller of a public server, with no pass-through header,
// upstream credential, or session selection, relays the upstream's own cache
// hints. Every other shape is labelled private, and tools/list additionally
// zeroes the ttl wherever a filter (mcp:connect on private servers, session
// selection on either) is attached to its chain.
func TestProxyManagerLabelsListResultsCallerVarying(t *testing.T) {
	t.Parallel()

	const (
		toolsListRequest      = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
		resourcesListRequest  = `{"jsonrpc":"2.0","id":3,"method":"resources/list","params":{}}`
		toolsListUpstream     = `{"jsonrpc":"2.0","id":2,"result":{"ttlMs":60000,"cacheScope":"public","tools":[{"name":"a","inputSchema":{}},{"name":"b","inputSchema":{}}]}}`
		resourcesListUpstream = `{"jsonrpc":"2.0","id":3,"result":{"ttlMs":60000,"cacheScope":"public","resources":[{"name":"a","uri":"file:///a"}]}}`
	)

	passThrough := []remotemcprepo.RemoteMcpServerHeader{{
		Name:                   "X-Tenant",
		ValueFromRequestHeader: pgtype.Text{String: "X-Caller-Tenant", Valid: true},
	}}

	cases := []struct {
		name         string
		visibility   string
		selection    *toolfilter.SessionSelection
		anonymous    bool
		headers      []remotemcprepo.RemoteMcpServerHeader
		upstreamAuth string
		// wantUpstream reports that both lists relay with the upstream's
		// own hints.
		wantUpstream bool
		// toolsFiltered reports that a filter is attached to the tools/list
		// chain, which zeroes the tools/list ttl.
		toolsFiltered bool
	}{
		{name: "public anonymous", visibility: mcpservers.VisibilityPublic, anonymous: true, wantUpstream: true},
		{name: "public authenticated", visibility: mcpservers.VisibilityPublic},
		{name: "public anonymous with session selection", visibility: mcpservers.VisibilityPublic, anonymous: true, selection: selectionOf(t, "a"), toolsFiltered: true},
		{name: "public anonymous with pass-through header", visibility: mcpservers.VisibilityPublic, anonymous: true, headers: passThrough},
		{name: "public anonymous with upstream credential", visibility: mcpservers.VisibilityPublic, anonymous: true, upstreamAuth: "upstream-token"},
		{name: "public authenticated with session selection", visibility: mcpservers.VisibilityPublic, selection: selectionOf(t, "a"), toolsFiltered: true},
		{name: "private", visibility: mcpservers.VisibilityPrivate, toolsFiltered: true},
		{name: "private with anonymous option", visibility: mcpservers.VisibilityPrivate, anonymous: true, toolsFiltered: true},
		{name: "private with session selection", visibility: mcpservers.VisibilityPrivate, selection: selectionOf(t, "a"), toolsFiltered: true},
	}
	for _, tc := range cases {
		for _, list := range []struct {
			method   string
			request  string
			upstream string
			filtered bool
		}{
			{method: "tools/list", request: toolsListRequest, upstream: toolsListUpstream, filtered: tc.toolsFiltered},
			{method: "resources/list", request: resourcesListRequest, upstream: resourcesListUpstream, filtered: false},
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
				var options []remotemcp.BuildOption
				if tc.anonymous {
					options = append(options, remotemcp.WithAnonymousCaller())
				}
				p := manager.Build(
					logger,
					&remotemcprepo.RemoteMcpServer{ID: uuid.New(), Url: upstream.URL},
					uuid.NewString(),
					tc.headers,
					tc.visibility,
					"organization-id",
					"project-id",
					tc.upstreamAuth,
					"",
					tc.selection,
					options...,
				)

				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(list.request))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json, text/event-stream")
				rr := httptest.NewRecorder()
				require.NoError(t, p.Post(rr, req))

				if tc.wantUpstream {
					require.Equal(t, list.upstream, rr.Body.String(),
						"an anonymous public result must relay the upstream's own cache hints")
					return
				}

				var envelope struct {
					Result map[string]json.RawMessage `json:"result"`
				}
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &envelope), rr.Body.String())
				require.JSONEq(t, `"private"`, string(envelope.Result["cacheScope"]),
					"a result shaped by Gram must never fall back to the public cache default")
				wantTTL := `60000`
				if list.filtered {
					wantTTL = `0`
				}
				require.JSONEq(t, wantTTL, string(envelope.Result["ttlMs"]))
				if tc.selection != nil && list.method == "tools/list" {
					require.JSONEq(t, `[{"name":"a","inputSchema":{}}]`, string(envelope.Result["tools"]),
						"the session selection filter must have run")
				}
			})
		}
	}
}
