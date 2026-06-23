package tunnels

import (
	"context"
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

func TestServeTunnel_ForwardsToGateway(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Stub gateway: echoes the request and the tunnel-ID header it received.
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte(r.Header.Get(wire.HeaderTunnelID) + "|" + r.Method + " " + r.URL.Path + "|" + string(body)))
	}))
	defer gw.Close()

	routes := route.NewMemory()
	require.NoError(t, routes.Publish(context.Background(), "t1", strings.TrimPrefix(gw.URL, "http://"), 30e9))

	svc := NewService(routes, logger)
	rec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc.ServeTunnel(w, r, "t1") // tunnelID injected server-side, never from caller
	}))
	defer rec.Close()

	resp, err := http.Post(rec.URL+"/mcp/call", "application/json", strings.NewReader(`{"x":1}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, `t1|POST /mcp/call|{"x":1}`, string(out))
}

func TestServeTunnel_NoRoute(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(route.NewMemory(), logger)
	rec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		svc.ServeTunnel(w, r, "missing")
	}))
	defer rec.Close()

	resp, err := http.Get(rec.URL + "/mcp/x")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	require.Equal(t, "no-route", resp.Header.Get("X-Gram-Tunnel-Error"))
}
