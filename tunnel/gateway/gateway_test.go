package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestPublicHandlerDoesNotForward(t *testing.T) {
	t.Parallel()

	gw := New(Config{}, NewStaticKeyStore(map[string]string{}), route.NewMemory(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")

	gw.PublicHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func newForwardTestGateway(t *testing.T, cfg Config) *Gateway {
	t.Helper()
	return New(cfg, NewStaticKeyStore(map[string]string{}), route.NewMemory(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestForwardHandlerRejectsMissingOrWrongToken(t *testing.T) {
	t.Parallel()

	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret"})

	for _, token := range []string{"", "wrong"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
		req.Header.Set(wire.HeaderTunnelID, "tunnel-1")
		if token != "" {
			req.Header.Set(wire.HeaderTunnelForwardToken, token)
		}

		gw.ForwardHandler().ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
	}
}

func TestForwardHandlerAcceptsValidTokenAndStripsIt(t *testing.T) {
	t.Parallel()

	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")
	req.Header.Set(wire.HeaderTunnelForwardToken, "s3cret")

	gw.ForwardHandler().ServeHTTP(rec, req)

	// Token accepted: the request advances past the gate to the registry
	// lookup, which has no live session and returns the distinct 502.
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "no-live-session", rec.Header().Get("X-Gram-Tunnel-Error"))
	require.Empty(t, req.Header.Get(wire.HeaderTunnelForwardToken))
}

func TestForwardHandlerAllowsAllWhenTokenUnset(t *testing.T) {
	t.Parallel()

	gw := newForwardTestGateway(t, Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")

	gw.ForwardHandler().ServeHTTP(rec, req)

	// No token configured: enforcement disabled, request reaches the lookup.
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "no-live-session", rec.Header().Get("X-Gram-Tunnel-Error"))
}

func TestParseServiceMetadata(t *testing.T) {
	t.Parallel()

	metadata, err := parseServiceMetadata(`{"environment":"prod","blank":"","empty_key":"ok"," ":"ignored"}`)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"environment": "prod",
		"empty_key":   "ok",
	}, metadata)
}

func TestParseServiceMetadataRejectsOversizedMetadata(t *testing.T) {
	t.Parallel()

	_, err := parseServiceMetadata(`{"value":"` + strings.Repeat("a", wire.MaxServiceMetadataBytes) + `"}`)
	require.ErrorIs(t, err, errServiceMetadataTooLarge)
}
