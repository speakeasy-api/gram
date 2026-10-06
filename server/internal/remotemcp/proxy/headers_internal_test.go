package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
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

func TestApplyRequestHeadersUserAuthorizationOverrideWinsConfiguredAuthorization(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{
				Name:                   "Authorization",
				StaticValue:            "Bearer agent-token",
				ValueFromRequestHeader: "",
				IsRequired:             true,
			},
			{
				Name:                   "X-Configured",
				StaticValue:            "preserved",
				ValueFromRequestHeader: "",
				IsRequired:             false,
			},
		},
		AuthorizationOverride: "user-token",
	}

	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	require.NoError(t, err)
	require.Equal(t, "Bearer user-token", remoteReq.Header.Get("Authorization"))
	require.Equal(t, "preserved", remoteReq.Header.Get("X-Configured"))
}

func TestApplyRequestHeadersConfiguredAuthorizationAppliesWithoutOverride(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{
				Name:                   "Authorization",
				StaticValue:            "Basic agent-credential",
				ValueFromRequestHeader: "",
				IsRequired:             true,
			},
		},
		AuthorizationOverride: "",
	}

	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	require.NoError(t, err)
	require.Equal(t, "Basic agent-credential", remoteReq.Header.Get("Authorization"))
}

func TestApplyRequestHeadersOverridePreservesNonAuthorizationConfiguredHeader(t *testing.T) {
	t.Parallel()

	userReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://gram.test/mcp", nil)
	remoteReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://upstream.test/mcp", nil)
	p := &Proxy{
		Logger: testenv.NewLogger(t),
		Headers: []ConfiguredHeader{
			{
				Name:                   "X-API-Key",
				StaticValue:            "configured-key",
				ValueFromRequestHeader: "",
				IsRequired:             true,
			},
		},
		AuthorizationOverride: "user-token",
	}

	err := p.applyRequestHeaders(t.Context(), userReq, remoteReq)
	require.NoError(t, err)
	require.Equal(t, "Bearer user-token", remoteReq.Header.Get("Authorization"))
	require.Equal(t, "configured-key", remoteReq.Header.Get("X-API-Key"))
}
