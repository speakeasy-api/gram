package mcp_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// handshakeUpstream is a scripted member that records the handshake dispatch runs against it.
type handshakeUpstream struct {
	url       string
	sessionID string
	// initializeError answers initialize with a JSON-RPC error envelope instead of a result.
	initializeError bool
	// truncatesInitialize mints a session on initialize, then drops the connection mid-body.
	truncatesInitialize bool
	mu                  sync.Mutex
	requests            []handshakeRequest
}

type handshakeRequest struct {
	method    string
	rpcMethod string
	session   string
	version   string
}

func newHandshakeUpstream(t *testing.T, sessionID string, initializeError bool) *handshakeUpstream {
	t.Helper()
	u := &handshakeUpstream{url: "", sessionID: sessionID, initializeError: initializeError, truncatesInitialize: false, mu: sync.Mutex{}, requests: nil}
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
	}
	_ = json.Unmarshal(body, &rpc)
	u.mu.Lock()
	u.requests = append(u.requests, handshakeRequest{method: r.Method, rpcMethod: rpc.Method, session: r.Header.Get("Mcp-Session-Id"), version: r.Header.Get("MCP-Protocol-Version")})
	u.mu.Unlock()

	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusOK)
		return
	}
	reply := func(envelope map[string]any) {
		envelope["jsonrpc"] = "2.0"
		envelope["id"] = rpc.ID
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(envelope)
	}
	switch rpc.Method {
	case "initialize":
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
		reply(map[string]any{"result": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "scripted", "version": "1"}}})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
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

// Dispatch acknowledges every session the member minted, even when initialize answered with a JSON-RPC error; only the consent probe judges the body.
func TestServePublic_MetaEndpoint_DispatchAcknowledgesMintedSessionAfterInitializeError(t *testing.T) {
	t.Parallel()

	upstream := newHandshakeUpstream(t, "sess-"+uuid.NewString()[:8], true)
	requests := driveMemberTool(t, upstream, "initerror")

	acked := acks(requests)
	require.Len(t, acked, 1, "requests: %+v", requests)
	require.Equal(t, upstream.sessionID, acked[0].session, "the acknowledgment rides the minted session")
}

// Dispatch sends no acknowledgment to a stateless member: with no session there is nothing to initialize.
func TestServePublic_MetaEndpoint_DispatchSkipsAcknowledgmentForStatelessMember(t *testing.T) {
	t.Parallel()

	upstream := newHandshakeUpstream(t, "", false)
	requests := driveMemberTool(t, upstream, "stateless")

	require.Empty(t, acks(requests), "requests: %+v", requests)
	for _, r := range requests {
		require.NotEqual(t, http.MethodDelete, r.method, "a stateless member has no session to close")
	}
}

// A session minted on an initialize whose body then fails is captured from the
// upstream response, so dispatch still closes it instead of stranding it.
func TestServePublic_MetaEndpoint_DispatchClosesSessionMintedBeforeBodyFailure(t *testing.T) {
	t.Parallel()

	upstream := newHandshakeUpstream(t, "sess-"+uuid.NewString()[:8], false)
	upstream.truncatesInitialize = true
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
