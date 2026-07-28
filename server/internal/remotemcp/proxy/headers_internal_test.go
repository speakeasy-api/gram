package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestApplyResponseHeadersStripsTunnelError: X-Gram-Tunnel-Error is internal
// gateway→gram-server wire vocabulary consumed by the retry policy. It must
// never relay to external MCP clients, or the internal statuses become a
// de-facto public contract.
func TestApplyResponseHeadersStripsTunnelError(t *testing.T) {
	t.Parallel()

	upstream := &http.Response{
		Header: http.Header{
			"X-Gram-Tunnel-Error": []string{"no-live-session"},
			"Content-Type":        []string{"application/json"},
		},
	}

	rec := httptest.NewRecorder()
	applyResponseHeaders(rec, upstream, "")

	require.Empty(t, rec.Header().Get("X-Gram-Tunnel-Error"))
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
