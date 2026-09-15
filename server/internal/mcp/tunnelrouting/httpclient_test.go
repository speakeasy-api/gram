package tunnelrouting

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
	"github.com/stretchr/testify/require"
)

func newTestHTTPClient(t *testing.T, routes route.Store) *HTTPClient {
	t.Helper()
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	return NewHTTPClient(routes, "forward-token", policy, []string{"127.0.0.0/8"})
}

func tokenRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, target, strings.NewReader("grant_type=authorization_code"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestHTTPClientForwardsPathQueryBodyAndHeaders(t *testing.T) {
	t.Parallel()

	var seen atomic.Pointer[http.Request]
	var seenBody atomic.Pointer[string]
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		seenBody.Store(&s)
		seen.Store(r.Clone(t.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gateway.URL, time.Minute))

	req := tokenRequest(t, "https://as.customer.internal/oauth/token?tenant=t1")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	got := seen.Load()
	require.NotNil(t, got)
	require.Equal(t, "/oauth/token", got.URL.Path)
	require.Equal(t, "tenant=t1", got.URL.RawQuery)
	require.Equal(t, "tunnel-1", got.Header.Get(wire.HeaderTunnelID))
	require.Equal(t, "forward-token", got.Header.Get(wire.HeaderTunnelForwardToken))
	require.Equal(t, "1", got.Header.Get(wire.HeaderTunnelRequireActive))
	require.NotEqual(t, "as.customer.internal", got.Host)
	require.Equal(t, "grant_type=authorization_code", *seenBody.Load())
}

func TestHTTPClientNoLiveSessionUnpublishesRoute(t *testing.T) {
	t.Parallel()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorNoLiveSession)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer gateway.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gateway.URL, time.Minute))

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1") //nolint:bodyclose // error path returns a nil response
	require.ErrorIs(t, err, ErrNoTunnelRoute)
	require.Nil(t, resp)

	candidates, err := routes.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestHTTPClientTunnelBusyKeepsRoutePublished(t *testing.T) {
	t.Parallel()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorTunnelBusy)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer gateway.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gateway.URL, time.Minute))

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1") //nolint:bodyclose // error path returns a nil response
	require.ErrorIs(t, err, ErrNoTunnelRoute)
	require.Nil(t, resp)

	candidates, err := routes.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{gateway.URL}, candidates)
}

func TestHTTPClientFailsOverToAnotherGateway(t *testing.T) {
	t.Parallel()

	// Route selection for anonymous requests is random, so both gateways
	// share one counter: whichever is dialed first reports busy, and the
	// failover lands on the other. Both orders exercise the same behavior.
	var requests atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorTunnelBusy)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	gatewayA := httptest.NewServer(handler)
	defer gatewayA.Close()
	gatewayB := httptest.NewServer(handler)
	defer gatewayB.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gatewayA.URL, time.Minute))
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gatewayB.URL, time.Minute))

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int64(2), requests.Load())
}

func TestHTTPClientNoLiveSessionUnpublishFailureStillFailsOver(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorNoLiveSession)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	gatewayA := httptest.NewServer(handler)
	defer gatewayA.Close()
	gatewayB := httptest.NewServer(handler)
	defer gatewayB.Close()

	routeTable := route.NewRouteTable()
	require.NoError(t, routeTable.Publish(t.Context(), "tunnel-1", gatewayA.URL, time.Minute))
	require.NoError(t, routeTable.Publish(t.Context(), "tunnel-1", gatewayB.URL, time.Minute))
	routes := &unpublishErrorStore{Store: routeTable, err: errors.New("route store unavailable"), calls: 0}

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int64(2), requests.Load())
	require.Equal(t, 1, routes.calls)
}

func TestHTTPClientSubstreamFailedIsNeverReplayed(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorSubstreamFailed)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer gateway.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gateway.URL, time.Minute))

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	require.Equal(t, wire.TunnelErrorSubstreamFailed, resp.Header.Get(ErrorHeader))
	require.Equal(t, int64(1), requests.Load())
}

func TestHTTPClientActiveCheckFailureFailsOverWithoutUnpublishing(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set(wire.HeaderTunnelError, wire.TunnelErrorActiveCheckFailed)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	gatewayA := httptest.NewServer(handler)
	defer gatewayA.Close()
	gatewayB := httptest.NewServer(handler)
	defer gatewayB.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gatewayA.URL, time.Minute))
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gatewayB.URL, time.Minute))

	resp, err := newTestHTTPClient(t, routes).Do(tokenRequest(t, "https://as.customer.internal/oauth/token"), "tunnel-1")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int64(2), requests.Load())

	candidates, err := routes.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Len(t, candidates, 2)
}

func TestHTTPClientNoRoutes(t *testing.T) {
	t.Parallel()

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, route.NewRouteTable()).Do(req, "tunnel-1") //nolint:bodyclose // error path returns a nil response
	require.ErrorIs(t, err, ErrNoTunnelRoute)
	require.Nil(t, resp)
}

func TestHTTPClientUnreachableGatewayFailsOverToALiveOne(t *testing.T) {
	t.Parallel()

	// Route selection for anonymous requests is random, so the assertion is
	// the one that holds in both orders: the forward succeeds. When the dead
	// address is drawn first it can only succeed by failing over, and
	// TestHTTPClientAllGatewaysUnreachable pins the unpublish that goes with it.
	var requests atomic.Int64
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()

	// A pod that died without unpublishing: the address is still in the store
	// but nothing accepts a connection there.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", deadURL, time.Minute))
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", live.URL, time.Minute))

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int64(1), requests.Load(), "the live gateway must see the request exactly once")

	candidates, err := routes.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Contains(t, candidates, live.URL, "a live route must never be unpublished")
}

func TestHTTPClientAllGatewaysUnreachable(t *testing.T) {
	t.Parallel()

	// One candidate, so the dial failure is reached deterministically: it must
	// drop the route rather than leave a dead pod's address to stall the next
	// token exchange too.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", deadURL, time.Minute))

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := newTestHTTPClient(t, routes).Do(req, "tunnel-1") //nolint:bodyclose // error path returns a nil response
	require.ErrorIs(t, err, ErrNoTunnelRoute)
	require.Nil(t, resp)

	candidates, err := routes.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestHTTPClientBlockedGatewayAddressIsNotUnpublished(t *testing.T) {
	t.Parallel()

	// The gateway CIDR allowlist is empty, so the loopback address the route
	// store carries is refused by the egress policy rather than by the pod.
	// That says nothing about the route and must leave it published.
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), "tunnel-1", gateway.URL, time.Minute))

	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	client := NewHTTPClient(routes, "forward-token", policy, nil)

	req := tokenRequest(t, "https://as.customer.internal/oauth/token")
	resp, err := client.Do(req, "tunnel-1") //nolint:bodyclose // error path returns a nil response
	require.Error(t, err)
	require.Nil(t, resp)
	require.NotErrorIs(t, err, ErrNoTunnelRoute, "an egress rejection is not an exhausted route pool")

	candidates, err := routes.Candidates(t.Context(), "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{gateway.URL}, candidates)
}
