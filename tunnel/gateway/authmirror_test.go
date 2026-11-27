package gateway

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/route"
)

// authMirrorTestRefresh is short so tests see several refreshes quickly.
const authMirrorTestRefresh = 20 * time.Millisecond

// errOperationsDown stands in for an operations instance that refuses every
// command.
var errOperationsDown = errors.New("operations instance is down")

// downRouteStore fails every command, like an unreachable operations instance.
type downRouteStore struct{}

func (downRouteStore) Publish(context.Context, string, string, time.Duration) error {
	return errOperationsDown
}

func (downRouteStore) Candidates(context.Context, string) ([]string, error) {
	return nil, errOperationsDown
}

func (downRouteStore) Unpublish(context.Context, string, string) error { return errOperationsDown }

func (downRouteStore) Delete(context.Context, string) error { return errOperationsDown }

// heldRouteStore blocks every command until its context ends, like an
// operations instance held by an instance move.
type heldRouteStore struct{}

func (heldRouteStore) Publish(ctx context.Context, _, _ string, _ time.Duration) error {
	<-ctx.Done()
	return ctx.Err()
}

func (heldRouteStore) Candidates(ctx context.Context, _ string) ([]string, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (heldRouteStore) Unpublish(ctx context.Context, _, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func (heldRouteStore) Delete(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

// fakeEligibility answers CarriesOAuth from a set, or fails when err is set.
type fakeEligibility struct {
	mu     sync.Mutex
	oauth  map[string]bool
	err    error
	checks int
}

func newFakeEligibility(oauthTunnels ...string) *fakeEligibility {
	oauth := make(map[string]bool, len(oauthTunnels))
	for _, tunnelID := range oauthTunnels {
		oauth[tunnelID] = true
	}
	return &fakeEligibility{mu: sync.Mutex{}, oauth: oauth, err: nil, checks: 0}
}

func (e *fakeEligibility) CarriesOAuth(_ context.Context, tunnelID string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.checks++
	if e.err != nil {
		return false, e.err
	}
	return e.oauth[tunnelID], nil
}

func (e *fakeEligibility) fail(err error) {
	e.mu.Lock()
	e.err = err
	e.mu.Unlock()
}

type authMirrorHarness struct {
	gateway *Gateway
	public  *httptest.Server
	owner   string
}

func newAuthMirrorHarness(
	t *testing.T,
	keys map[string]string,
	bulk route.Store,
	mirror route.Store,
	eligibility AuthRouteEligibility,
) *authMirrorHarness {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw, err := New(Config{
		AuthzPublicKeys:      "",
		ForwardToken:         testForwardToken,
		AuthRoutes:           &AuthRouteMirror{Routes: mirror, Eligibility: eligibility},
		routeRefreshInterval: authMirrorTestRefresh,
	}, NewStaticKeyStore(keys), bulk, logger)
	require.NoError(t, err)
	publicServer := httptest.NewServer(gw.PublicHandler())
	forwardServer := httptest.NewServer(gw.ForwardHandler())
	owner := forwardServer.Listener.Addr().String()
	gw.SetAdvertiseAddr(owner)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		gw.Drain(ctx)
		gw.CloseSessions(ctx)
		forwardServer.Close()
		publicServer.Close()
	})
	return &authMirrorHarness{gateway: gw, public: publicServer, owner: owner}
}

func (h *authMirrorHarness) connect(t *testing.T, key string) *testAgent {
	t.Helper()
	agent, _, err := dialTestAgent(t.Context(), h.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	return agent
}

func TestAuthMirrorPublishesOnlyOAuthTunnels(t *testing.T) {
	t.Parallel()

	bulk := newRecordingRouteStore(false, false)
	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{
		"tunnel-oauth": "gram_tunnel_mirror_oauth",
		"tunnel-plain": "gram_tunnel_mirror_plain",
	}, bulk, mirror, newFakeEligibility("tunnel-oauth"))

	harness.connect(t, "gram_tunnel_mirror_oauth")
	harness.connect(t, "gram_tunnel_mirror_plain")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		oauth, _ := mirror.Candidates(t.Context(), "tunnel-oauth")
		plainBulk, _ := bulk.Candidates(t.Context(), "tunnel-plain")
		assert.Equal(c, []string{harness.owner}, oauth)
		assert.Equal(c, []string{harness.owner}, plainBulk)
	}, 5*time.Second, 10*time.Millisecond)

	// Several refreshes later the plain tunnel is still only on the bulk store.
	require.Eventually(t, func() bool { return mirror.operationCount("tunnel-oauth", "publish") >= 3 }, 5*time.Second, 10*time.Millisecond)
	plain, err := mirror.Candidates(t.Context(), "tunnel-plain")
	require.NoError(t, err)
	require.Empty(t, plain)
	require.Zero(t, mirror.operationCount("tunnel-plain", "publish"))
}

func TestAuthMirrorKeepsPublishingWhileOperationsIsDown(t *testing.T) {
	t.Parallel()

	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{"tunnel-oauth": "gram_tunnel_mirror_down"},
		downRouteStore{}, mirror, newFakeEligibility("tunnel-oauth"))

	harness.connect(t, "gram_tunnel_mirror_down")

	require.Eventually(t, func() bool { return mirror.operationCount("tunnel-oauth", "publish") >= 3 }, 5*time.Second, 10*time.Millisecond)
	candidates, err := mirror.Candidates(t.Context(), "tunnel-oauth")
	require.NoError(t, err)
	require.Equal(t, []string{harness.owner}, candidates)
}

func TestAuthMirrorKeepsPublishingWhileOperationsIsHeldForAMove(t *testing.T) {
	t.Parallel()

	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{"tunnel-oauth": "gram_tunnel_mirror_held"},
		heldRouteStore{}, mirror, newFakeEligibility("tunnel-oauth"))

	harness.connect(t, "gram_tunnel_mirror_held")

	// The reconciler's own publish waits out its five-second budget against the
	// held store; the mirror refreshes many times meanwhile.
	require.Eventually(t, func() bool { return mirror.operationCount("tunnel-oauth", "publish") >= 5 }, 3*time.Second, 10*time.Millisecond)
	candidates, err := mirror.Candidates(t.Context(), "tunnel-oauth")
	require.NoError(t, err)
	require.Equal(t, []string{harness.owner}, candidates)
}

func TestAuthMirrorWithdrawsRouteWhenAgentDisconnects(t *testing.T) {
	t.Parallel()

	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{"tunnel-oauth": "gram_tunnel_mirror_withdraw"},
		newRecordingRouteStore(false, false), mirror, newFakeEligibility("tunnel-oauth"))

	agent := harness.connect(t, "gram_tunnel_mirror_withdraw")
	require.Eventually(t, func() bool {
		candidates, _ := mirror.Candidates(t.Context(), "tunnel-oauth")
		return len(candidates) == 1
	}, 5*time.Second, 10*time.Millisecond)

	agent.Close()
	require.Eventually(t, func() bool {
		candidates, _ := mirror.Candidates(t.Context(), "tunnel-oauth")
		return len(candidates) == 0 && mirror.operationCount("tunnel-oauth", "unpublish") == 1
	}, 5*time.Second, 10*time.Millisecond)
}

func TestAuthMirrorDeletesRouteWhenTunnelIsRevoked(t *testing.T) {
	t.Parallel()

	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{"tunnel-oauth": "gram_tunnel_mirror_revoke"},
		newRecordingRouteStore(false, false), mirror, newFakeEligibility("tunnel-oauth"))

	harness.connect(t, "gram_tunnel_mirror_revoke")
	// Another gateway's route for the same tunnel goes too.
	require.NoError(t, mirror.Publish(t.Context(), "tunnel-oauth", "other-gateway", routeTTL))
	require.Eventually(t, func() bool {
		candidates, _ := mirror.Candidates(t.Context(), "tunnel-oauth")
		return len(candidates) == 2
	}, 5*time.Second, 10*time.Millisecond)

	// RevokeTunnel returns only after the mirror's delete was attempted.
	harness.gateway.RevokeTunnel(t.Context(), "tunnel-oauth")
	require.Equal(t, 1, mirror.operationCount("tunnel-oauth", "delete"))
	candidates, err := mirror.Candidates(t.Context(), "tunnel-oauth")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestAuthMirrorRetriesFailedRevocationDelete(t *testing.T) {
	t.Parallel()

	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{"tunnel-oauth": "gram_tunnel_mirror_retry"},
		newRecordingRouteStore(false, false), mirror, newFakeEligibility("tunnel-oauth"))

	harness.connect(t, "gram_tunnel_mirror_retry")
	require.Eventually(t, func() bool { return mirror.operationCount("tunnel-oauth", "publish") >= 1 }, 5*time.Second, 10*time.Millisecond)

	mirror.failNextDelete()
	harness.gateway.RevokeTunnel(t.Context(), "tunnel-oauth")
	require.Equal(t, 1, mirror.operationCount("tunnel-oauth", "delete_failed"))
	require.Eventually(t, func() bool { return mirror.operationCount("tunnel-oauth", "delete") == 1 }, 5*time.Second, 10*time.Millisecond)
	candidates, err := mirror.Candidates(t.Context(), "tunnel-oauth")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestAuthMirrorDrainRemovesItsRoutes(t *testing.T) {
	t.Parallel()

	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{"tunnel-oauth": "gram_tunnel_mirror_drain"},
		newRecordingRouteStore(false, false), mirror, newFakeEligibility("tunnel-oauth"))

	harness.connect(t, "gram_tunnel_mirror_drain")
	require.Eventually(t, func() bool {
		candidates, _ := mirror.Candidates(t.Context(), "tunnel-oauth")
		return len(candidates) == 1
	}, 5*time.Second, 10*time.Millisecond)

	harness.gateway.Drain(t.Context())
	candidates, err := mirror.Candidates(t.Context(), "tunnel-oauth")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestAuthMirrorSkipsTunnelWhoseEligibilityIsUnknown(t *testing.T) {
	t.Parallel()

	eligibility := newFakeEligibility("tunnel-oauth")
	eligibility.fail(errors.New("database unavailable"))
	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{"tunnel-oauth": "gram_tunnel_mirror_unknown"},
		newRecordingRouteStore(false, false), mirror, eligibility)

	harness.connect(t, "gram_tunnel_mirror_unknown")
	require.Eventually(t, func() bool {
		eligibility.mu.Lock()
		defer eligibility.mu.Unlock()
		return eligibility.checks >= 3
	}, 5*time.Second, 10*time.Millisecond)
	require.Zero(t, mirror.operationCount("tunnel-oauth", "publish"))

	// Once the lookup answers, the tunnel is mirrored.
	eligibility.fail(nil)
	require.Eventually(t, func() bool {
		candidates, _ := mirror.Candidates(t.Context(), "tunnel-oauth")
		return len(candidates) == 1
	}, 5*time.Second, 10*time.Millisecond)
}

func TestAuthMirrorKeepsKnownTunnelThroughEligibilityFailure(t *testing.T) {
	t.Parallel()

	eligibility := newFakeEligibility("tunnel-oauth")
	mirror := newRecordingRouteStore(false, false)
	harness := newAuthMirrorHarness(t, map[string]string{"tunnel-oauth": "gram_tunnel_mirror_known"},
		newRecordingRouteStore(false, false), mirror, eligibility)

	harness.connect(t, "gram_tunnel_mirror_known")
	require.Eventually(t, func() bool { return mirror.operationCount("tunnel-oauth", "publish") >= 1 }, 5*time.Second, 10*time.Millisecond)

	// Expire the cached answer so every pass asks again, and fail every ask.
	mirrorLoop := harness.gateway.authRoutes
	mirrorLoop.mu.Lock()
	mirrorLoop.now = func() time.Time { return time.Now().Add(2 * authRouteRecheckInterval) }
	mirrorLoop.mu.Unlock()
	eligibility.fail(errors.New("database unavailable"))
	published := mirror.operationCount("tunnel-oauth", "publish")
	require.Eventually(t, func() bool { return mirror.operationCount("tunnel-oauth", "publish") >= published+3 }, 5*time.Second, 10*time.Millisecond)
	require.Zero(t, mirror.operationCount("tunnel-oauth", "unpublish"))
}

func TestGatewayRejectsIncompleteAuthMirror(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := New(Config{
		ForwardToken: testForwardToken,
		AuthRoutes:   &AuthRouteMirror{Routes: route.NewRouteTable(), Eligibility: nil},
	}, NewStaticKeyStore(map[string]string{}), route.NewRouteTable(), logger)
	require.Error(t, err)
}
