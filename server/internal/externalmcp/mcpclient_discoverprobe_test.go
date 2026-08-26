package externalmcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// handshakeRecorder is a minimal Streamable HTTP MCP server that counts the
// HTTP attempts made per JSON-RPC method. It answers the legacy initialize
// handshake and delegates every other method to the supplied responder, which
// decides the status code for that attempt.
type handshakeRecorder struct {
	mu       sync.Mutex
	attempts map[string]int

	// respond reports the status code to answer with for the given method and
	// 1-based attempt number. A zero return means the recorder answers the
	// method itself.
	respond func(method string, attempt int) int
}

func newHandshakeRecorder(respond func(method string, attempt int) int) *handshakeRecorder {
	return &handshakeRecorder{attempts: map[string]int{}, respond: respond}
}

func (h *handshakeRecorder) attemptsFor(method string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attempts[method]
}

func (h *handshakeRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	h.attempts[envelope.Method]++
	attempt := h.attempts[envelope.Method]
	h.mu.Unlock()

	if status := h.respond(envelope.Method, attempt); status != 0 {
		w.WriteHeader(status)
		return
	}

	// Notifications carry no id and must be acknowledged with 202 and no body.
	if len(envelope.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Mcp-Session-Id", "test-session")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      envelope.ID,
		"result": map[string]any{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "recorder", "version": "1.0.0"},
		},
	})
}

func newDiscoverProbeClient(t *testing.T, handler http.Handler) (*Client, error) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// An unsafe policy is required so the client may dial httptest's loopback
	// listener; the default policy blocks 127.0.0.0/8.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	return NewClient(
		t.Context(),
		testenv.NewLogger(t),
		policy,
		server.URL,
		types.TransportTypeStreamableHTTP,
		&ClientOptions{DisableRetries: false},
	)
}

// A server that predates MCP 2026-07-28 may answer the server/discover
// capability probe with a 5xx. That answer will not change on a retry, and the
// SDK responds to it by falling back to the legacy initialize handshake, so
// the probe must be attempted exactly once rather than spending the full
// retry budget first.
func TestNewClientDoesNotRetryRejectedDiscoverProbe(t *testing.T) {
	t.Parallel()

	recorder := newHandshakeRecorder(func(method string, _ int) int {
		if method == methodServerDiscover {
			return http.StatusInternalServerError
		}
		return 0
	})

	client, err := newDiscoverProbeClient(t, recorder)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.Equal(t, 1, recorder.attemptsFor(methodServerDiscover), "the capability probe must not be retried")
	require.Equal(t, 1, recorder.attemptsFor("initialize"), "the legacy handshake must run once the probe is refused")
}

// The retry budget is withheld from the capability probe only. A failure on
// any other request, the fallback handshake included, must still be retried.
func TestNewClientRetriesNonDiscoverFailures(t *testing.T) {
	t.Parallel()

	recorder := newHandshakeRecorder(func(method string, attempt int) int {
		switch {
		case method == methodServerDiscover:
			return http.StatusInternalServerError
		case method == "initialize" && attempt == 1:
			return http.StatusInternalServerError
		default:
			return 0
		}
	})

	client, err := newDiscoverProbeClient(t, recorder)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.Equal(t, 1, recorder.attemptsFor(methodServerDiscover), "the capability probe must not be retried")
	require.Equal(t, 2, recorder.attemptsFor("initialize"), "a failed legacy handshake must be retried")
}
