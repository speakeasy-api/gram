package mcp_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// handshakeUpstream is a scripted member that records the handshake dispatch runs against it.
type handshakeUpstream struct {
	handshakeUpstreamConfig
	url      string
	mu       sync.Mutex
	requests []handshakeRequest
}

// handshakeUpstreamConfig is fixed before the server starts serving requests.
type handshakeUpstreamConfig struct {
	sessionID       string
	protocolVersion string
	listPages       int
	discoveryStatus int
	// initializeError answers initialize with a JSON-RPC error envelope instead of a result.
	initializeError bool
	// truncatesInitialize mints a session on initialize, then drops the connection mid-body.
	truncatesInitialize bool
}

type handshakeRequest struct {
	method    string
	rpcMethod string
	session   string
	version   string
	cursor    string
}

func newHandshakeUpstream(t *testing.T, config handshakeUpstreamConfig) *handshakeUpstream {
	t.Helper()
	if config.protocolVersion == "" {
		config.protocolVersion = mcpversions.Version20250618
	}
	u := &handshakeUpstream{handshakeUpstreamConfig: config}
	srv := httptest.NewServer(http.HandlerFunc(u.serve))
	t.Cleanup(srv.Close)
	u.url = srv.URL
	return u
}

func (u *handshakeUpstream) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var rpc struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Cursor string                     `json:"cursor"`
			Name   string                     `json:"name"`
			Meta   map[string]json.RawMessage `json:"_meta"`
		} `json:"params"`
	}
	_ = json.Unmarshal(body, &rpc)
	u.mu.Lock()
	u.requests = append(u.requests, handshakeRequest{method: r.Method, rpcMethod: rpc.Method, session: r.Header.Get("Mcp-Session-Id"), version: r.Header.Get("MCP-Protocol-Version"), cursor: rpc.Params.Cursor})
	u.mu.Unlock()

	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusOK)
		return
	}
	if u.protocolVersion == mcpversions.Version20260728 {
		var version string
		_ = json.Unmarshal(rpc.Params.Meta[sdk.MetaKeyProtocolVersion], &version)
		if version != u.protocolVersion || r.Header.Get("MCP-Protocol-Version") != version || r.Header.Get("Mcp-Method") != rpc.Method || len(rpc.Params.Meta[sdk.MetaKeyClientInfo]) == 0 || len(rpc.Params.Meta[sdk.MetaKeyClientCapabilities]) == 0 {
			http.Error(w, "missing or inconsistent modern request metadata", http.StatusBadRequest)
			return
		}
		if rpc.Method == "tools/call" && r.Header.Get("Mcp-Name") != rpc.Params.Name {
			http.Error(w, "missing mirrored tool name", http.StatusBadRequest)
			return
		}
	}
	reply := func(envelope map[string]any) {
		envelope["jsonrpc"] = "2.0"
		envelope["id"] = rpc.ID
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelope)
	}
	switch rpc.Method {
	case "server/discover":
		if u.discoveryStatus != 0 {
			w.WriteHeader(u.discoveryStatus)
			return
		}
		if u.protocolVersion != mcpversions.Version20260728 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reply(map[string]any{"result": map[string]any{"supportedVersions": []string{u.protocolVersion}, "capabilities": map[string]any{"tools": map[string]any{}}, "_meta": map[string]any{"io.modelcontextprotocol/serverInfo": map[string]any{"name": "scripted", "version": "1"}}}})
	case "initialize":
		if u.protocolVersion == mcpversions.Version20260728 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if u.sessionID != "" {
			w.Header().Set("Mcp-Session-Id", u.sessionID)
		}
		if u.truncatesInitialize {
			// The session is allocated before the body fails.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":`))
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
			if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		if u.initializeError {
			// A live member that answers initialize with a JSON-RPC error yet mints a session and serves calls on it.
			reply(map[string]any{"error": map[string]any{"code": -32602, "message": "unsupported client capabilities"}})
			return
		}
		reply(map[string]any{"result": map[string]any{"protocolVersion": u.protocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "scripted", "version": "1"}}})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		if u.listPages > 0 {
			page, _ := strconv.Atoi(rpc.Params.Cursor)
			next := ""
			if page+1 < u.listPages {
				next = strconv.Itoa(page + 1)
			}
			reply(map[string]any{"result": map[string]any{"tools": []map[string]any{{"name": fmt.Sprintf("tool_%d", page), "inputSchema": map[string]any{"type": "object"}}}, "nextCursor": next}})
			return
		}
		reply(map[string]any{"result": map[string]any{"tools": []map[string]any{{"name": "ping", "description": "pong", "inputSchema": map[string]any{"type": "object"}}}}})
	case "tools/call":
		reply(map[string]any{"result": map[string]any{"content": []map[string]any{{"type": "text", "text": "pong"}}}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (u *handshakeUpstream) drain() []handshakeRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	got := u.requests
	u.requests = nil
	return got
}

func acks(requests []handshakeRequest) []handshakeRequest {
	var out []handshakeRequest
	for _, r := range requests {
		if r.rpcMethod == "notifications/initialized" {
			out = append(out, r)
		}
	}
	return out
}

func driveMemberTool(t *testing.T, upstream *handshakeUpstream, memberSuffix string) []handshakeRequest {
	t.Helper()
	text, isError, requests := driveMemberToolResult(t, upstream, memberSuffix)
	require.False(t, isError, "execute_tool result: %s", text)
	require.Contains(t, text, "pong")
	return requests
}

func driveMemberToolResult(t *testing.T, upstream *handshakeUpstream, memberSuffix string) (string, bool, []handshakeRequest) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "meta-handshake-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	memberSlug := "member-" + memberSuffix + "-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "scripted member", memberSlug, 1, upstream.url)

	envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{"name": memberSlug + "--ping", "arguments": map[string]any{}})
	text, isError := metaToolResultText(t, envelope)
	return text, isError, upstream.drain()
}

// Failed initialization never dispatches a tool, and the SDK closes the minted session.
func TestServePublic_MetaEndpoint_DispatchRejectsInitializeError(t *testing.T) {
	t.Parallel()

	upstream := newHandshakeUpstream(t, handshakeUpstreamConfig{sessionID: "sess-" + uuid.NewString()[:8], initializeError: true})
	text, isError, requests := driveMemberToolResult(t, upstream, "initerror")
	require.True(t, isError)
	require.Contains(t, text, "unsupported client capabilities")
	require.Empty(t, acks(requests))
	deletes := 0
	for _, r := range requests {
		require.NotEqual(t, "tools/call", r.rpcMethod)
		if r.method == http.MethodDelete {
			deletes++
		}
	}
	require.Equal(t, 1, deletes)

}

// Legacy initialization is acknowledged even when the member does not allocate a session.
func TestServePublic_MetaEndpoint_DispatchAcknowledgesStatelessLegacyMember(t *testing.T) {
	t.Parallel()

	upstream := newHandshakeUpstream(t, handshakeUpstreamConfig{})
	requests := driveMemberTool(t, upstream, "stateless")

	require.Len(t, acks(requests), 1, "requests: %+v", requests)
	for _, r := range requests {
		require.NotEqual(t, http.MethodDelete, r.method, "a stateless member has no session to close")
	}
}

// A session minted on an initialize whose body then fails is captured from the
// upstream response, so dispatch still closes it instead of stranding it.
func TestServePublic_MetaEndpoint_DispatchClosesSessionMintedBeforeBodyFailure(t *testing.T) {
	t.Parallel()

	upstream := newHandshakeUpstream(t, handshakeUpstreamConfig{sessionID: "sess-" + uuid.NewString()[:8], truncatesInitialize: true})
	text, isError, requests := driveMemberToolResult(t, upstream, "truncated")
	require.True(t, isError, "a member that drops mid-initialize is a member error: %s", text)

	deletes := 0
	for _, r := range requests {
		if r.method == http.MethodDelete {
			deletes++
			require.Equal(t, upstream.sessionID, r.session, "the DELETE names the session the failed initialize minted")
		}
	}
	require.Equal(t, 1, deletes, "requests: %+v", requests)
}

func TestServePublic_MetaEndpoint_MemberProtocolRevisions(t *testing.T) {
	t.Parallel()
	for _, version := range mcpversions.All() {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			sessionID := "legacy-session"
			if version == mcpversions.Version20260728 {
				sessionID = ""
			}
			upstream := newHandshakeUpstream(t, handshakeUpstreamConfig{sessionID: sessionID, protocolVersion: version})
			requests := driveMemberTool(t, upstream, "revision")
			require.NotEmpty(t, requests)
			require.Equal(t, "server/discover", requests[0].rpcMethod)
			calls, deletes := 0, 0
			for _, req := range requests {
				if req.method == http.MethodDelete {
					deletes++
				}
				if req.rpcMethod == "tools/call" {
					calls++
					require.Equal(t, version, req.version)
					require.Equal(t, sessionID, req.session)
				}
				if version == mcpversions.Version20260728 {
					require.NotEqual(t, "initialize", req.rpcMethod)
					require.NotEqual(t, "notifications/initialized", req.rpcMethod)
					require.Empty(t, req.session)
				}
			}
			require.Equal(t, 1, calls)
			if version == mcpversions.Version20260728 {
				require.Zero(t, deletes)
			} else {
				require.Equal(t, 1, deletes)
			}
		})
	}
}

func TestServePublic_MetaEndpoint_MemberPagination(t *testing.T) {
	t.Parallel()
	for _, pages := range []int{2, 10} {
		t.Run(strconv.Itoa(pages), func(t *testing.T) {
			t.Parallel()
			upstream := newHandshakeUpstream(t, handshakeUpstreamConfig{sessionID: "pagination-session", listPages: pages})
			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			slug := "meta-pages-" + uuid.NewString()[:8]
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
			seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "paged", "paged", 1, upstream.url)
			envelope := callMetaTool(t, ctx, ti, slug, "describe_server", map[string]any{"server": "paged"})
			text, isError := metaToolResultText(t, envelope)
			require.False(t, isError, text)
			requests := upstream.drain()
			initializes, lists, deletes := 0, 0, 0
			for _, req := range requests {
				if req.method == http.MethodDelete {
					deletes++
				}
				if req.rpcMethod == "initialize" {
					initializes++
				}
				if req.rpcMethod == "tools/list" {
					require.Equal(t, "pagination-session", req.session)
					if lists == 0 {
						require.Empty(t, req.cursor)
					} else {
						require.Equal(t, strconv.Itoa(lists), req.cursor)
					}
					lists++
				}
			}
			require.Equal(t, 1, initializes)
			require.Equal(t, min(pages, 8), lists)
			require.Equal(t, 1, deletes)
		})
	}
}

func TestServePublic_MetaEndpoint_MemberDiscoveryFallback(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			upstream := newHandshakeUpstream(t, handshakeUpstreamConfig{sessionID: "fallback-session", discoveryStatus: status})
			requests := driveMemberTool(t, upstream, "discovery-fallback")
			discovers, calls := 0, 0
			for _, req := range requests {
				if req.rpcMethod == "server/discover" {
					discovers++
				}
				if req.rpcMethod == "tools/call" {
					calls++
				}
			}
			require.Equal(t, 1, discovers, "capability probing must not acquire a retry budget")
			require.Equal(t, 1, calls)
		})
	}
}
