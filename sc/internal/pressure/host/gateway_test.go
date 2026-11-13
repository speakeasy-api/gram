package host_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sc/internal/pressure/host"
)

// These are the tests of gateway_test.go in PR #8377, with Postgres, the
// outbox, organization encryption and OpenTelemetry replaced by the fakes in
// fakes_test.go. TestGatewayProxiesWithIdentityAndAttestation is not ported:
// it tests the request path, not the lifecycle.

type gatewayFixture struct {
	db      *fakeDB
	runtime *fakeRuntime
	metrics *fakeMetrics
	clock   *fakeClock
	gateway *host.Gateway
}

func newGatewayFixture(t *testing.T, ordinal int32, configure ...func(*host.Config, *fakeRuntime)) *gatewayFixture {
	t.Helper()
	db := newFakeDB()
	runtime := &fakeRuntime{}
	metrics := newFakeMetrics()
	clock := &fakeClock{}
	cfg := host.Config{
		Ordinal: ordinal, PodName: "gram-tailnet-gateway-0", Shard: host.DefaultShard, StateDir: t.TempDir(),
		Tag: host.DefaultTag, PollInterval: host.DefaultPollInterval, DrainTimeout: time.Second, StartTimeout: 5 * time.Second,
		MaxHosts: 10, Now: clock.Now,
	}
	for _, apply := range configure {
		apply(&cfg, runtime)
	}
	gateway, err := host.New(slog.New(slog.DiscardHandler), db, metrics, cfg, runtime)
	require.NoError(t, err)
	return &gatewayFixture{db: db, runtime: runtime, metrics: metrics, clock: clock, gateway: gateway}
}

func (f *gatewayFixture) waitForHosts(t *testing.T, count int) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Len(c, f.gateway.Hosts(), count)
	}, 10*time.Second, 20*time.Millisecond)
}

func TestGatewayHostsIngressAtOrdinalAndHeartbeats(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, orgID := f.db.seedIngress("gateway-host")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)

	nodes := f.runtime.started()
	require.Len(t, nodes, 1)
	require.Equal(t, "speakeasy-gw-0", nodes[0].spec.Hostname)
	require.Equal(t, "svc:private", nodes[0].spec.ServiceName, "service name follows the ingress label")
	require.Equal(t, "tag:speakeasy", nodes[0].spec.Tag)
	require.Equal(t, "tskey-auth-one-shot", nodes[0].spec.AuthKey)
	require.EqualValues(t, 0, nodes[0].spec.Slot)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.db.row(ingressID, 0)
		assert.Equal(c, string(host.NodeStatusStarting), row.status)
		assert.Equal(c, "Starting", row.lastError, "the first probe records the backend state")
	}, 5*time.Second, 20*time.Millisecond)
	row := f.db.row(ingressID, 0)
	require.NotNil(t, row.authKey, "key stays until the node is online")
	require.Equal(t, "speakeasy.example.ts.net", row.dnsName)
	require.Equal(t, "gram-tailnet-gateway-0", row.podName)

	nodes[0].setOnline("nSTABLE")
	f.gateway.Poll(ctx)
	row = f.db.row(ingressID, 0)
	require.Equal(t, string(host.NodeStatusOnline), row.status)
	require.Equal(t, "nSTABLE", row.nodeID)
	require.False(t, row.connectedSince.IsZero())
	require.False(t, row.lastSeenAt.IsZero())
	require.Nil(t, row.authKey, "one-shot key is cleared once state exists")
	require.Equal(t, 1, f.db.wakesFor(orgID), "coming online wakes the reconciler once")

	f.gateway.Poll(ctx)
	require.Equal(t, 1, f.db.wakesFor(orgID), "steady state does not wake the reconciler again")
	require.Len(t, f.runtime.started(), 1, "a hosted ingress is not started twice")
	gauge := f.metrics.gauge("gateway-host")
	require.Len(t, gauge, 1)
	require.Equal(t, 1, gauge[host.NodeStatusOnline], "the hosts gauge counts one online host")
}

func TestGatewayWaitsForAuthKeyAndReconnectsWithState(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, orgID := f.db.seedIngress("gateway-key")
	f.db.seedSlot(ingressID, 0, "")

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.db.row(ingressID, 0)
		assert.Equal(c, string(host.NodeStatusPending), row.status)
		assert.Equal(c, "awaiting auth key", row.lastError)
	}, 5*time.Second, 20*time.Millisecond)
	require.Empty(t, f.runtime.started())
	require.Empty(t, f.gateway.Hosts())

	store := newTestStateStore(t, f.db, ingressID, orgID, 0)
	require.NoError(t, store.WriteState(machineKeyStateKey, []byte("existing")))
	require.NoError(t, store.WriteState(knownProfilesStateKey, []byte(profileList)))

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	nodes := f.runtime.started()
	require.Len(t, nodes, 1)
	require.Empty(t, nodes[0].spec.AuthKey, "persisted state reconnects without a key")
}

func TestGatewayStopsHostWhenIngressIsDisabled(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, orgID := f.db.seedIngress("gateway-stop")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	node := f.runtime.started()[0]
	node.setOnline("nSTABLE")
	f.gateway.Poll(ctx)

	f.db.setEnabled(ingressID, false)
	f.gateway.Poll(ctx)
	require.Empty(t, f.gateway.Hosts())
	require.True(t, node.isClosed())
	row := f.db.row(ingressID, 0)
	require.Equal(t, string(host.NodeStatusStopped), row.status)
	require.True(t, row.connectedSince.IsZero())
	require.Equal(t, 2, f.db.wakesFor(orgID), "going online and offline each wake the reconciler")
	require.Empty(t, f.metrics.gauge("gateway-stop"), "a stopped host leaves the hosts gauge")
}

func TestGatewaySkipsRetiredSlotsAndForeignOrdinals(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 2)
	ingressID, _ := f.db.seedIngress("gateway-retired")
	f.db.seedSlot(ingressID, 0, "tskey-auth-slot0")

	f.gateway.Poll(ctx)
	require.Never(t, func() bool { return len(f.gateway.Hosts()) > 0 }, 200*time.Millisecond, 20*time.Millisecond)
	require.Empty(t, f.runtime.started(), "ordinal 2 has no slot row")

	f.db.seedSlot(ingressID, 2, "tskey-auth-slot2")
	f.db.retire(ingressID, 2)
	f.gateway.Poll(ctx)
	require.Never(t, func() bool { return len(f.gateway.Hosts()) > 0 }, 200*time.Millisecond, 20*time.Millisecond)
	require.Empty(t, f.runtime.started(), "retired slots are never hosted")
}

func TestGatewayStopsRunningHostWhenSlotIsRetired(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, _ := f.db.seedIngress("gateway-retire")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	node := f.runtime.started()[0]

	f.db.retire(ingressID, 0)
	f.gateway.Poll(ctx)
	require.Empty(t, f.gateway.Hosts(), "a retired slot is no longer desired")
	require.True(t, node.isClosed())
	require.Equal(t, string(host.NodeStatusStopped), f.db.row(ingressID, 0).status)
}

func TestGatewayResetsPartialStateAfterFailedRegistration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	f.runtime.failStart = errors.New("login timed out")
	f.runtime.writePartial = true
	ingressID, orgID := f.db.seedIngress("gateway-partial")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.db.row(ingressID, 0)
		assert.Equal(c, string(host.NodeStatusError), row.status)
		assert.Nil(c, row.authKey, "a consumed one-shot key is cleared so the reconciler mints another")
	}, 5*time.Second, 20*time.Millisecond)
	_, err := newTestStateStore(t, f.db, ingressID, orgID, 0).ReadState(machineKeyStateKey)
	require.ErrorIs(t, err, errStateNotExist, "partial state is reset")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, f.db.wakesFor(orgID), "the reconciler is woken to mint a new key")
	}, 5*time.Second, 20*time.Millisecond)
}

func TestGatewayRecordsStartFailures(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	f.runtime.failStart = errors.New("control plane unreachable")
	ingressID, _ := f.db.seedIngress("gateway-fail")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.db.row(ingressID, 0)
		assert.Equal(c, string(host.NodeStatusError), row.status)
		assert.Contains(c, row.lastError, "control plane unreachable")
	}, 5*time.Second, 20*time.Millisecond)
	require.Empty(t, f.gateway.Hosts())
}

func TestGatewayKeepsIdentityWhenStartFailsAfterLogin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	f.runtime.failStart = errors.New("advertise tailscale service: refused")
	f.runtime.writeLogin = true
	ingressID, orgID := f.db.seedIngress("gateway-keep-login")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, string(host.NodeStatusError), f.db.row(ingressID, 0).status)
	}, 5*time.Second, 20*time.Millisecond)
	registered, err := newTestStateStore(t, f.db, ingressID, orgID, 0).Registered(ctx)
	require.NoError(t, err)
	require.True(t, registered, "a login that completed is a real device and is kept")
	require.Zero(t, f.db.wakesFor(orgID), "no new key is needed")

	f.runtime.setFailStart(nil)
	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	nodes := f.runtime.started()
	require.Len(t, nodes, 1)
	require.Empty(t, nodes[0].spec.AuthKey, "the retry reconnects as the same device")
}

func TestGatewayStopsHostThatNeedsLogin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, orgID := f.db.seedIngress("gateway-needs-login")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	node := f.runtime.started()[0]
	node.setOnline("nREMOVED")
	f.gateway.Poll(ctx)

	node.setNeedsLogin()
	f.gateway.Poll(ctx)
	require.Empty(t, f.gateway.Hosts())
	require.True(t, node.isClosed())
	row := f.db.row(ingressID, 0)
	require.Equal(t, string(host.NodeStatusNeedsLogin), row.status)
	require.Equal(t, "nREMOVED", row.nodeID, "the reconciler needs the old device id")
	require.GreaterOrEqual(t, f.db.wakesFor(orgID), 2, "the reconciler is woken to replace the identity")

	// Until the reconciler resets the slot, the refused identity is not retried.
	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.db.row(ingressID, 0)
		assert.Equal(c, string(host.NodeStatusNeedsLogin), row.status)
		assert.Equal(c, "awaiting re-registration", row.lastError)
	}, 5*time.Second, 20*time.Millisecond)
	require.Len(t, f.runtime.started(), 1)
}

func TestGatewayMarksRefusedIdentityAtStart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	f.runtime.failStart = fmt.Errorf("bring tsnet node up: %w", host.ErrNodeNeedsLogin)
	ingressID, orgID := f.db.seedIngress("gateway-refused")
	f.db.seedSlot(ingressID, 0, "")
	store := newTestStateStore(t, f.db, ingressID, orgID, 0)
	require.NoError(t, store.WriteState(knownProfilesStateKey, []byte(profileList)))

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, string(host.NodeStatusNeedsLogin), f.db.row(ingressID, 0).status)
	}, 5*time.Second, 20*time.Millisecond)
	registered, err := store.Registered(ctx)
	require.NoError(t, err)
	require.True(t, registered, "the gateway leaves the reset to the reconciler")
}

func TestGatewayFillsHostCapInOnePoll(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0, func(cfg *host.Config, _ *fakeRuntime) { cfg.MaxHosts = 2 })
	for _, name := range []string{"gateway-cap-a", "gateway-cap-b", "gateway-cap-c"} {
		ingressID, _ := f.db.seedIngress(name)
		f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")
	}

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 2)
	require.Never(t, func() bool { return len(f.gateway.Hosts()) > 2 }, 200*time.Millisecond, 20*time.Millisecond)
	require.Len(t, f.runtime.started(), 2, "one poll fills the cap, and no further")
}

func TestGatewayStopsHostsConcurrently(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const closeDelay = 400 * time.Millisecond
	f := newGatewayFixture(t, 0, func(_ *host.Config, r *fakeRuntime) { r.closeDelay = closeDelay })
	var ids []uuid.UUID
	for _, name := range []string{"gateway-stop-a", "gateway-stop-b", "gateway-stop-c"} {
		ingressID, _ := f.db.seedIngress(name)
		f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")
		ids = append(ids, ingressID)
	}
	f.gateway.Poll(ctx)
	f.waitForHosts(t, 3)

	for _, id := range ids {
		f.db.setEnabled(id, false)
	}
	started := time.Now()
	f.gateway.Poll(ctx)
	elapsed := time.Since(started)
	require.Empty(t, f.gateway.Hosts())
	require.Less(t, elapsed, 2*closeDelay, "three stops cost about one close, not three")
}

func TestGatewayReportsHostsAwaitingACertificate(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, orgID := f.db.seedIngress("gateway-cert")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	node := f.runtime.started()[0]

	const reason = "Let's Encrypt is rate limiting new certificates for this tailnet; retrying after 2026-11-12 22:53 UTC"
	node.setOnline("nCERT")
	node.setUncertified(reason)
	f.gateway.Poll(ctx)
	row := f.db.row(ingressID, 0)
	require.Equal(t, string(host.NodeStatusAwaitingCertificate), row.status)
	require.Equal(t, reason, row.lastError, "the slot records why and when the node retries")
	require.True(t, row.connectedSince.IsZero(), "a host without a certificate is not serving")
	require.Equal(t, 1, f.db.wakesFor(orgID), "the reconciler learns about the missing certificate right away")

	f.gateway.Poll(ctx)
	require.Equal(t, 1, f.db.wakesFor(orgID), "still waiting does not wake the reconciler again")

	node.setUncertified("")
	f.gateway.Poll(ctx)
	require.Equal(t, string(host.NodeStatusOnline), f.db.row(ingressID, 0).status)
	require.Equal(t, 2, f.db.wakesFor(orgID), "coming online wakes the reconciler")
}

func TestGatewayReportsHostsAwaitingApproval(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, orgID := f.db.seedIngress("gateway-approval")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	node := f.runtime.started()[0]

	node.setOnline("nWAITING")
	node.setApproved(false)
	f.gateway.Poll(ctx)
	row := f.db.row(ingressID, 0)
	require.Equal(t, string(host.NodeStatusAwaitingApproval), row.status)
	require.Contains(t, row.lastError, "svc:private")
	require.True(t, row.connectedSince.IsZero(), "an unapproved host is not serving")
	require.Equal(t, 1, f.db.wakesFor(orgID), "the reconciler learns about the missing approval right away")
	require.NotNil(t, row.authKey, "the key is only cleared once the host is online")

	f.gateway.Poll(ctx)
	require.Equal(t, 1, f.db.wakesFor(orgID), "staying unapproved does not wake the reconciler again")

	node.setApproved(true)
	f.gateway.Poll(ctx)
	row = f.db.row(ingressID, 0)
	require.Equal(t, string(host.NodeStatusOnline), row.status)
	require.False(t, row.connectedSince.IsZero())
	require.Equal(t, 2, f.db.wakesFor(orgID))

	node.setApproved(false)
	f.gateway.Poll(ctx)
	require.Equal(t, string(host.NodeStatusAwaitingApproval), f.db.row(ingressID, 0).status)
	require.Equal(t, 3, f.db.wakesFor(orgID))
	require.Len(t, f.gateway.Hosts(), 1, "the node stays up so a later approval takes effect")
	require.Len(t, f.runtime.started(), 1)
}

func TestGatewayRestartsHostWhoseNodeFailed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, orgID := f.db.seedIngress("gateway-node-fail")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")
	died := errors.New("service listener stopped: listener died")
	latest := func() *fakeNode {
		nodes := f.runtime.started()
		return nodes[len(nodes)-1]
	}

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	latest().setOnline("nONE")
	f.gateway.Poll(ctx)
	require.Equal(t, 1, f.db.wakesFor(orgID))

	latest().failStatus(died)
	f.gateway.Poll(ctx)
	require.Empty(t, f.gateway.Hosts())
	require.True(t, latest().isClosed())
	row := f.db.row(ingressID, 0)
	require.Equal(t, string(host.NodeStatusError), row.status)
	require.Contains(t, row.lastError, "listener died")
	require.Equal(t, 2, f.db.wakesFor(orgID))

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	require.Len(t, f.runtime.started(), 2)
	require.Empty(t, latest().spec.AuthKey, "the persisted identity reconnects without a key")

	latest().failStatus(died)
	f.gateway.Poll(ctx)
	require.Empty(t, f.gateway.Hosts())
	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	require.Len(t, f.runtime.started(), 3)
	latest().failStatus(died)
	f.gateway.Poll(ctx)
	require.Empty(t, f.gateway.Hosts())
	f.gateway.Poll(ctx)
	require.Never(t, func() bool { return len(f.gateway.Hosts()) > 0 }, 200*time.Millisecond, 20*time.Millisecond)
	require.Len(t, f.runtime.started(), 3, "the third failure in a row backs the slot off")
}

// TestGatewayRetriesAfterBackoff is new: the original checked only that a
// slot backs off, not that it comes back.
func TestGatewayRetriesAfterBackoff(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	f.runtime.failStart = errors.New("control plane unreachable")
	start := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	f.clock.set(start)
	ingressID, _ := f.db.seedIngress("gateway-backoff")
	f.db.seedSlot(ingressID, 0, "")
	require.NoError(t, newTestStateStore(t, f.db, ingressID, "", 0).WriteState(knownProfilesStateKey, []byte(profileList)))

	attempts := func() int {
		return f.metrics.lifecycleCount("start/error")
	}
	settled := func(want int) func(c *assert.CollectT) {
		return func(c *assert.CollectT) {
			assert.Equal(c, want, attempts())
			assert.Equal(c, string(host.NodeStatusError), f.db.row(ingressID, 0).status)
		}
	}
	for want := 1; want <= 3; want++ {
		f.gateway.Poll(ctx)
		require.EventuallyWithT(t, settled(want), 5*time.Second, 10*time.Millisecond)
	}
	f.clock.set(start.Add(4 * time.Minute))
	f.gateway.Poll(ctx)
	require.Never(t, func() bool { return attempts() > 3 }, 100*time.Millisecond, 10*time.Millisecond, "still backing off")

	f.clock.set(start.Add(5 * time.Minute))
	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, settled(4), 5*time.Second, 10*time.Millisecond, "the first poll after the backoff retries")
}

func TestGatewayRunDrainsOnCancel(t *testing.T) {
	t.Parallel()
	f := newGatewayFixture(t, 0)
	ingressID, _ := f.db.seedIngress("gateway-drain")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.gateway.Run(ctx) }()
	f.waitForHosts(t, 1)
	require.True(t, f.gateway.Ready())
	require.NoError(t, f.gateway.Healthy())

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("gateway did not drain")
	}
	require.False(t, f.gateway.Ready())
	require.True(t, f.runtime.started()[0].isClosed())
	require.Equal(t, string(host.NodeStatusStopped), f.db.row(ingressID, 0).status)
}

// TestGatewayWatchdogReportsAStalledLoop is new: the original only checked
// Healthy on a fresh loop.
func TestGatewayWatchdogReportsAStalledLoop(t *testing.T) {
	t.Parallel()
	f := newGatewayFixture(t, 0)
	start := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	f.clock.set(start)
	require.NoError(t, f.gateway.Healthy(), "no poll yet is not a stall")
	f.gateway.Poll(t.Context())
	f.clock.set(start.Add(3 * host.DefaultPollInterval))
	require.NoError(t, f.gateway.Healthy())
	f.clock.set(start.Add(3*host.DefaultPollInterval + time.Second))
	require.Error(t, f.gateway.Healthy())
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	valid := host.Config{
		Ordinal: 0, Shard: "shared", StateDir: "/tmp/x", Tag: "tag:speakeasy",
		PollInterval: time.Second, DrainTimeout: time.Second, StartTimeout: time.Second, MaxHosts: 1,
	}
	require.NoError(t, valid.Validate())
	cases := map[string]func(c *host.Config){
		"negative ordinal": func(c *host.Config) { c.Ordinal = -1 },
		"empty shard":      func(c *host.Config) { c.Shard = "" },
		"no tag":           func(c *host.Config) { c.Tag = "" },
		"zero hosts":       func(c *host.Config) { c.MaxHosts = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := valid
			mutate(&cfg)
			require.Error(t, cfg.Validate())
		})
	}
}

func TestGatewayRecordsUnavailableOrganizationKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, orgID := f.db.seedIngress("gateway-suspended")
	f.db.seedSlot(ingressID, 0, "")
	require.NoError(t, newTestStateStore(t, f.db, ingressID, orgID, 0).WriteState(knownProfilesStateKey, []byte(`{"p1":{}}`)))
	f.db.suspend(orgID)

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.db.row(ingressID, 0)
		assert.Equal(c, string(host.NodeStatusError), row.status)
		assert.Equal(c, "organization encryption key unavailable", row.lastError, "the row says why the host is not serving")
	}, 5*time.Second, 20*time.Millisecond)
	require.Empty(t, f.runtime.started(), "a node whose state cannot be read is not started")
}

func TestGatewayReportsCertificateExpiryAndRequests(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, 0)
	ingressID, _ := f.db.seedIngress("gateway-cert-metrics")
	f.db.seedSlot(ingressID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	require.Empty(t, f.metrics.collect(), "a node holding no certificate reports no expiry")

	node := f.runtime.started()[0]
	expiresAt := time.Now().Add(20 * 24 * time.Hour)
	node.mu.Lock()
	node.online, node.certExpiresAt = true, expiresAt
	events := node.spec.CertificateEvents
	node.mu.Unlock()
	f.gateway.Poll(ctx)

	observed := f.metrics.collect()
	require.Len(t, observed, 1)
	require.Equal(t, expiresAt, observed[0].ExpiresAt)
	require.Equal(t, ingressID, observed[0].IngressID, "each connection is its own series")
	require.EqualValues(t, 0, observed[0].Slot)

	require.NotNil(t, events, "the gateway counts the node's certificate requests")
	events(ctx, "renew", "error")
	events(ctx, "renew", "error")
	require.Equal(t, []certRequest{{"renew", "error"}, {"renew", "error"}}, f.metrics.certs)

	runCtx, cancel := context.WithCancel(ctx)
	cancel()
	require.NoError(t, f.gateway.Run(runCtx))
	require.Empty(t, f.metrics.collect(), "a stopped gateway reports no expiry")
}
