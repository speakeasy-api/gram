package mcp_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

func TestServeConsentMCP_RemoteDropsUpstreamBrowserStateHeaders(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		sse  bool
	}{
		{name: "json", sse: false},
		{name: "sse", sse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)

				var result map[string]any
				switch req.Method {
				case "initialize":
					result = map[string]any{
						"protocolVersion": mcpversions.Version20250618,
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "upstream", "version": "1.0.0"},
					}
				case "tools/list":
					result = map[string]any{
						"tools": []map[string]any{{"name": "upstream_tool", "inputSchema": map[string]any{"type": "object"}}},
					}
				default:
					result = map[string]any{}
				}
				payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}

				w.Header().Set("Mcp-Session-Id", "upstream-session")
				w.Header().Add("Set-Cookie", "planted=1; Path=/")
				w.Header().Add("Set-Cookie", "other=2; Path=/")
				w.Header().Set("Clear-Site-Data", `"cookies", "storage"`)
				if tc.sse {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(payload)
			}))
			t.Cleanup(upstream.Close)

			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			endpointSlug := "remote-consent-cookie-" + uuid.NewString()
			mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, issuer.ProjectID.UUID, upstream.URL, endpointSlug, "public", issuer.ID)
			stateID, csrfToken := seedModernConsentChallenge(t, ctx, ti, issuer.ID, client, mcpServer.ID, endpointSlug)
			endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, endpointSlug, "mcp")
			require.NoError(t, err)
			attempt := uuid.NewString()

			initialize := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrfToken, attempt,
				`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"consent-test","version":"1.0.0"}}}`, nil)
			list := serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrfToken, attempt,
				`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, map[string]string{
					mcpversions.HTTPHeader: mcpversions.Version20250618,
					"Mcp-Session-Id":       "upstream-session",
				})

			for name, w := range map[string]*httptest.ResponseRecorder{"initialize": initialize, "tools/list": list} {
				res := w.Result()
				require.Empty(t, res.Header.Values("Set-Cookie"), name)
				require.Empty(t, res.Header.Values("Clear-Site-Data"), name)
				require.Empty(t, res.Cookies(), name)
				require.Equal(t, "upstream-session", res.Header.Get("Mcp-Session-Id"), name)
			}
			require.Contains(t, initialize.Body.String(), `"serverInfo"`)
			require.Contains(t, list.Body.String(), `"upstream_tool"`)
			if tc.sse {
				require.Contains(t, list.Header().Get("Content-Type"), "text/event-stream")
			}
		})
	}
}
