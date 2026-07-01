package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestPublicHandlerDoesNotForward(t *testing.T) {
	t.Parallel()

	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")

	gw.PublicHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func newForwardTestGateway(t *testing.T, cfg Config) *Gateway {
	t.Helper()
	gw, err := New(cfg, NewStaticKeyStore(map[string]string{}), route.NewRouteTable(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	return gw
}

func TestNewRejectsMissingForwardToken(t *testing.T) {
	t.Parallel()

	_, err := New(Config{}, NewStaticKeyStore(map[string]string{}), route.NewRouteTable(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.ErrorIs(t, err, errMissingForwardToken)
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

	// 502 (not 403) means the token passed the gate and reached the no-session lookup.
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "no-live-session", rec.Header().Get("X-Gram-Tunnel-Error"))
	require.Empty(t, req.Header.Get(wire.HeaderTunnelForwardToken))
}

func TestForwardHandlerRejectsMissingForwardTokenConfig(t *testing.T) {
	t.Parallel()

	gw := &Gateway{
		cfg:      Config{},
		keys:     NewStaticKeyStore(map[string]string{}),
		routes:   route.NewRouteTable(),
		reg:      newRegistry(),
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		upgrader: websocket.Upgrader{},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")

	gw.ForwardHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
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
