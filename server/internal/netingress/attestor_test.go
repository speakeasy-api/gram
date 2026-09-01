package netingress

import (
	"bufio"
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type staticWorkloadVerifier struct {
	ingress Ingress
	token   string
}

func (v staticWorkloadVerifier) Verify(_ context.Context, token, source string) (Ingress, error) {
	if token != v.token || source == "" {
		return Ingress{}, ErrAttestationRejected
	}
	return v.ingress, nil
}

func TestAttestorHandlerForwardsOnlyPrivateRoutesAndRefreshesToken(t *testing.T) {
	t.Parallel()

	tokenPath := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("first-token\n"), 0o600))

	seen := make(chan *http.Request, 2)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Clone(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	handler := newTestAttestor(t, upstream, tokenPath)

	for _, token := range []string{"first-token", "rotated-token"} {
		req := httptest.NewRequest(http.MethodPost, "https://private.example.ts.net/mcp/server?tag=one", nil)
		req.Host = "private.example.ts.net"
		req.Header.Set("Authorization", "Bearer mcp-token")
		req.Header.Set(AttestationHeader, "Bearer forged")
		req.Header.Set(TailscaleUserLoginHeader, "person@example.com")
		req.Header.Set("Tailscale-Unsupported", "remove-me")
		req.Header.Set("X-Real-IP", "203.0.113.10")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNoContent, rr.Code)

		forwarded := <-seen
		require.Equal(t, "private.example.ts.net", forwarded.Host)
		require.Equal(t, "/mcp/server", forwarded.URL.Path)
		require.Equal(t, "tag=one", forwarded.URL.RawQuery)
		require.Equal(t, "Bearer "+token, forwarded.Header.Get(AttestationHeader))
		require.Equal(t, "Bearer mcp-token", forwarded.Header.Get("Authorization"))
		require.Equal(t, "person@example.com", forwarded.Header.Get(TailscaleUserLoginHeader))
		require.Empty(t, forwarded.Header.Get("Tailscale-Unsupported"))
		require.Empty(t, forwarded.Header.Get("X-Real-IP"))

		require.NoError(t, os.WriteFile(tokenPath, []byte("rotated-token\n"), 0o600))
	}
}

func TestAttestorHandlerRejectsWrongHostRouteAndMissingToken(t *testing.T) {
	t.Parallel()

	tokenPath := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("token"), 0o600))
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unexpected upstream request")
	}))
	t.Cleanup(upstream.Close)
	handler := newTestAttestor(t, upstream, tokenPath)

	tests := []struct {
		name   string
		method string
		path   string
		host   string
		status int
	}{
		{name: "wrong host", method: http.MethodPost, path: "/mcp/server", host: "other.example.ts.net", status: http.StatusNotFound},
		{name: "management route", method: http.MethodPost, path: "/rpc/organizations.list", host: "private.example.ts.net", status: http.StatusNotFound},
		{name: "runtime subroute", method: http.MethodPost, path: "/mcp/server/delete-all", host: "private.example.ts.net", status: http.StatusNotFound},
		{name: "wrong method", method: http.MethodPut, path: "/mcp/server", host: "private.example.ts.net", status: http.StatusNotFound},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, "https://"+tt.host+tt.path, nil)
		req.Host = tt.host
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		require.Equal(t, tt.status, rr.Code, tt.name)
	}

	require.NoError(t, os.Remove(tokenPath))
	req := httptest.NewRequest(http.MethodPost, "https://private.example.ts.net/mcp/server", nil)
	req.Host = "private.example.ts.net"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestAttestorHandlerStreamsAndCancels(t *testing.T) {
	t.Parallel()

	tokenPath := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("token"), 0o600))
	cancelled := make(chan struct{})
	privateRuntime := RouteGuard(Middleware(
		staticWorkloadVerifier{
			ingress: Ingress{
				ID:                     uuid.New(),
				OrganizationID:         "org_test",
				Provider:               ProviderTailscale,
				DNSName:                "private.example.ts.net",
				IdentityRequired:       false,
				AttestorNamespace:      "gram-test",
				AttestorServiceAccount: "netingress-test",
			},
			token: "token",
		},
		IdentityParsers{ProviderTailscale: TailscaleIdentityParser{}},
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: first\n\n")
		flusher.Flush()
		<-r.Context().Done()
		close(cancelled)
	})))
	upstream := httptest.NewTLSServer(privateRuntime)
	t.Cleanup(upstream.Close)

	server := httptest.NewServer(newTestAttestor(t, upstream, tokenPath))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/mcp/server", nil)
	require.NoError(t, err)
	req.Host = "private.example.ts.net"
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: first\n", line)
	cancel()

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}
}

func TestPrivateRouteCensus(t *testing.T) {
	t.Parallel()

	guarded := RouteGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/mcp/server"},
		{http.MethodGet, "/mcp/server"},
		{http.MethodDelete, "/mcp/server"},
		{http.MethodGet, "/mcp/server/install"},
		{http.MethodPost, "/mcp/server/register"},
		{http.MethodGet, "/mcp/server/authorize"},
		{http.MethodGet, "/mcp/server/connect"},
		{http.MethodPost, "/mcp/server/connect"},
		{http.MethodPost, "/mcp/server/connect/remote-session"},
		{http.MethodPost, "/mcp/server/connect/mcp"},
		{http.MethodDelete, "/mcp/server/connect/mcp"},
		{http.MethodGet, "/mcp/server/connect/first-party"},
		{http.MethodPost, "/mcp/server/token"},
		{http.MethodPost, "/mcp/server/revoke"},
		{http.MethodPost, "/x/mcp/server"},
		{http.MethodGet, "/.well-known/oauth-protected-resource/mcp/server"},
		{http.MethodGet, "/.well-known/oauth-authorization-server/x/mcp/server"},
		{http.MethodGet, "/mcp/install-page-deadbeef.js"},
		{http.MethodGet, "/mcp/consent-page-deadbeef.js"},
		{http.MethodGet, "/mcp/consent-tools-deadbeef.js"},
	}
	for _, route := range allowed {
		require.True(t, IsPrivateRoute(route.method, route.path), "%s %s", route.method, route.path)
		req := httptest.NewRequest(route.method, route.path, nil)
		rr := httptest.NewRecorder()
		guarded.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNoContent, rr.Code, "%s %s", route.method, route.path)
	}

	blocked := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/livez"},
		{http.MethodPost, "/rpc/organizations.list"},
		{http.MethodGet, "/admin"},
		{http.MethodPost, "/hooks/test"},
		{http.MethodGet, "/marketplace/repository"},
		{http.MethodGet, "/mcp/idp_callback"},
		{http.MethodGet, "/mcp/remote_login_callback"},
		{http.MethodGet, "/x/mcp/idp_callback"},
		{http.MethodGet, "/x/mcp/remote_login_callback"},
		{http.MethodPost, "/mcp/idp_callback/"},
		{http.MethodGet, "/x/mcp/idp_callback/"},
		{http.MethodGet, "/mcp/remote_login_callback/"},
		{http.MethodDelete, "/mcp/install-page-deadbeef.js/"},
		{http.MethodGet, "/oauth/callback"},
		{http.MethodGet, "/.well-known/oauth-client/id"},
		{http.MethodPost, "/mcp/server/install"},
		{http.MethodGet, "/mcp/server/token"},
		{http.MethodGet, "/mcp/server/unknown"},
		{http.MethodGet, "/mcp/server/install/extra"},
		{http.MethodGet, "/mcp//install"},
		{http.MethodGet, "/mcp/install-page-.js"},
	}
	for _, route := range blocked {
		require.False(t, IsPrivateRoute(route.method, route.path), "%s %s", route.method, route.path)
		req := httptest.NewRequest(route.method, route.path, nil)
		rr := httptest.NewRecorder()
		guarded.ServeHTTP(rr, req)
		require.Equal(t, http.StatusNotFound, rr.Code, "%s %s", route.method, route.path)
	}
}

func TestNewAttestorHandlerRequiresHTTPS(t *testing.T) {
	t.Parallel()

	target, err := url.Parse("http://gram-private.example")
	require.NoError(t, err)
	_, err = NewAttestorHandler(AttestorConfig{
		Upstream:     target,
		ExpectedHost: "private.example.ts.net",
		TokenPath:    "/token",
		Transport:    http.DefaultTransport,
		Logger:       nil,
	})
	require.ErrorContains(t, err, "must use HTTPS")
}

func TestNewAttestorTransportDisablesAmbientProxy(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)
	certificate := server.Certificate()
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	transport, err := NewAttestorTransport(caPEM)
	require.NoError(t, err)
	require.Nil(t, transport.Proxy)
	require.NotNil(t, transport.TLSClientConfig)
	require.NotNil(t, transport.TLSClientConfig.RootCAs)
}

func TestCanonicalAuthorityMatchesPrivateMiddleware(t *testing.T) {
	t.Parallel()

	host, err := canonicalAuthority("PRIVATE.EXAMPLE.TS.NET:443")
	require.NoError(t, err)
	require.Equal(t, "private.example.ts.net", host)
}

func TestReadProjectedTokenRejectsWhitespace(t *testing.T) {
	t.Parallel()

	for _, value := range []string{" token\n", "token \n", "token\tvalue\n", "token\nvalue"} {
		path := filepath.Join(t.TempDir(), "token")
		require.NoError(t, os.WriteFile(path, []byte(value), 0o600))
		_, err := readProjectedToken(path)
		require.Error(t, err, value)
	}
}

func newTestAttestor(t *testing.T, upstream *httptest.Server, tokenPath string) http.Handler {
	t.Helper()
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	handler, err := NewAttestorHandler(AttestorConfig{
		Upstream:     target,
		ExpectedHost: "private.example.ts.net",
		TokenPath:    tokenPath,
		Transport:    upstream.Client().Transport,
		Logger:       nil,
	})
	require.NoError(t, err)
	return handler
}
