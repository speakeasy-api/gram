package redisswitch

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/redisconn"
	"github.com/speakeasy-api/gram/tunnel/redisconn/redisconntest"
)

const (
	currentPassword = "current-password"
	nextPassword    = "next-password"
	nextToken       = "next-iam-token"

	// stateKey is the state key of the role most tests use.
	stateKey = stateKeyPrefix + DefaultRole

	// settle is how long a test waits for the poller to observe a change.
	settle = 5 * time.Second
)

func discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func connector(t *testing.T, endpoint redisconn.Endpoint, tokens *redisconntest.TokenSource) *redisconn.Connector {
	t.Helper()

	c, err := redisconn.NewConnector(endpoint, tokens)
	require.NoError(t, err)
	return c
}

func plain(addr string) redisconn.Endpoint {
	return redisconn.PasswordEndpoint(addr, "")
}

func binding(current, next string) Binding {
	return Binding{Role: DefaultRole, Current: plain(current), Next: plain(next), Tokens: nil}
}

type fixture struct {
	current *miniredis.Miniredis
	next    *miniredis.Miniredis
	sw      *Switch
	client  *redis.Client

	// ahead is added to the switch's clock, to age its last reading.
	ahead atomic.Int64

	// dials counts the sockets the switch opened, by address.
	dials *dialCounter
}

// dialCounter counts dials by address.
type dialCounter struct {
	mu    sync.Mutex
	count map[string]int
}

func (d *dialCounter) wrap(dial func(context.Context, string, string, time.Duration) (net.Conn, error)) func(context.Context, string, string, time.Duration) (net.Conn, error) {
	return func(ctx context.Context, network, addr string, timeout time.Duration) (net.Conn, error) {
		d.mu.Lock()
		d.count[addr]++
		d.mu.Unlock()
		return dial(ctx, network, addr, timeout)
	}
}

func (d *dialCounter) to(addr string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.count[addr]
}

// newSwitchFixture arms current, starts a switch between the two endpoints,
// and returns a client that follows it. Its clock can be moved ahead and its
// dials are counted; dials after a move are not delayed.
func newSwitchFixture(t *testing.T, current, next *miniredis.Miniredis, currentEndpoint, nextEndpoint redisconn.Endpoint, tokens *redisconntest.TokenSource, opts *redis.Options) *fixture {
	t.Helper()

	f := &fixture{current: current, next: next, sw: nil, client: nil, ahead: atomic.Int64{}, dials: &dialCounter{mu: sync.Mutex{}, count: map[string]int{}}}
	require.NoError(t, f.current.Set(stateKey, valueCurrent))

	sw, err := start(t.Context(), discard(), DefaultRole,
		connector(t, currentEndpoint, tokens),
		connector(t, nextEndpoint, tokens),
		opts.DialTimeout)
	require.NoError(t, err)
	sw.mu.Lock()
	sw.now = func() time.Time { return time.Now().Add(time.Duration(f.ahead.Load())) }
	sw.dialTCP = f.dials.wrap(dialTCP)
	sw.jitter = func() time.Duration { return 0 }
	sw.mu.Unlock()

	f.sw = sw
	f.client = sw.client(opts)
	t.Cleanup(func() { _ = f.client.Close() })

	return f
}

// newFixture starts two password-protected instances, arms the first, and
// returns a client that follows its switch keys. Different passwords prove the
// client authenticates to whichever instance it is using.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	current, next := miniredis.RunT(t), miniredis.RunT(t)
	current.RequireAuth(currentPassword)
	next.RequireAuth(nextPassword)

	return newSwitchFixture(t, current, next,
		redisconn.PasswordEndpoint(current.Addr(), currentPassword),
		redisconn.PasswordEndpoint(next.Addr(), nextPassword),
		nil,
		&redis.Options{DialTimeout: time.Second})
}

// tlsServer starts an instance that serves TLS for 127.0.0.1 with a
// certificate ca signed, and accepts only the IAM user with token.
func tlsServer(t *testing.T, ca *redisconntest.CA, token string) *miniredis.Miniredis {
	t.Helper()

	server, err := miniredis.RunTLS(ca.LoopbackServerConfig(t))
	require.NoError(t, err)
	t.Cleanup(server.Close)
	server.RequireUserAuth(redisconn.IAMUsername, token)
	return server
}

func iamEndpoint(addr string, ca *redisconntest.CA) redisconn.Endpoint {
	return redisconn.Endpoint{Addr: addr, Auth: redisconn.AuthIAM, Password: "", CACertPEM: ca.PEM, ServerName: ""}
}

func (f *fixture) eventually(t *testing.T, want state) {
	t.Helper()

	require.Eventually(t, func() bool {
		got, _ := f.sw.load()
		return got == want
	}, settle, 10*time.Millisecond)
}

func (f *fixture) hold(t *testing.T) {
	t.Helper()

	require.NoError(t, f.current.Set(HoldKey, "run-1"))
	f.eventually(t, stateHold)
}

func (f *fixture) move(t *testing.T) {
	t.Helper()

	require.NoError(t, f.current.Set(stateKey, movedPrefix+"run-1"))
	f.current.Del(HoldKey)
	f.eventually(t, stateNext)
}

func shortContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	t.Cleanup(cancel)

	return ctx
}

func TestNewClientWithoutNextIsAnOrdinaryClient(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	server.RequireAuth(currentPassword)
	client, err := NewClient(t.Context(), discard(), &redis.Options{}, Binding{
		Role:    DefaultRole,
		Current: redisconn.PasswordEndpoint(server.Addr(), currentPassword),
		Next:    plain(""),
		Tokens:  nil,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, server.Exists("k"))
	require.Equal(t, 3, client.Options().MaxRetries, "an unarmed client keeps the default retries")
	require.Equal(t, currentPassword, client.Options().Password)
	require.Nil(t, client.Options().TLSConfig)
}

func TestNewClientWithoutNextKeepsAnEmptyAddressAsBefore(t *testing.T) {
	t.Parallel()

	client, err := NewClient(t.Context(), discard(), &redis.Options{}, binding("", ""))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.Equal(t, "localhost:6379", client.Options().Addr, "go-redis's default, as before")
}

func TestNewClientWithoutNextRejectsUnknownAuthMode(t *testing.T) {
	t.Parallel()

	config := binding("localhost:6379", "")
	config.Current.Auth = "unknown"
	_, err := NewClient(t.Context(), discard(), &redis.Options{}, config)
	require.ErrorContains(t, err, "unknown auth mode")
}

func TestNewClientWithoutNextConnectsToATLSInstance(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	server := tlsServer(t, ca, nextToken)
	client, err := NewClient(t.Context(), discard(), &redis.Options{}, Binding{
		Role:    DefaultRole,
		Current: iamEndpoint(server.Addr(), ca),
		Next:    plain(""),
		Tokens:  redisconntest.NewTokenSource(nextToken, time.Hour),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, server.Exists("k"))
}

func TestNewClientWithNextEqualToCurrentIsAnOrdinaryClient(t *testing.T) {
	t.Parallel()

	// No state key: an armed client would refuse to start.
	server := miniredis.RunT(t)
	client, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(server.Addr(), server.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, server.Exists("k"))
	require.Equal(t, 3, client.Options().MaxRetries, "an unarmed client keeps the default retries")
}

func TestNewClientRefusesAnIAMNextWithoutTLS(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))

	_, err := NewClient(t.Context(), discard(), &redis.Options{}, Binding{
		Role:    DefaultRole,
		Current: plain(current.Addr()),
		Next:    redisconn.Endpoint{Addr: "127.0.0.1:1", Auth: redisconn.AuthIAM, Password: "", CACertPEM: "", ServerName: ""},
		Tokens:  redisconntest.NewTokenSource(nextToken, time.Hour),
	})
	require.ErrorIs(t, err, redisconn.ErrTokenOverPlaintext)
}

func TestStartRefusesAnUnarmedInstance(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	next := miniredis.RunT(t)

	_, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.ErrorIs(t, err, ErrNotArmed)
}

func TestStartFailsWhenTheKeysCannotBeRead(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	addr := server.Addr()
	server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	_, err := NewClient(ctx, discard(), &redis.Options{}, binding(addr, "127.0.0.1:1"))
	require.Error(t, err)
}

func TestStartFailsWhenTheNextInstanceRejectsThePassword(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	next := miniredis.RunT(t)
	next.RequireAuth(nextPassword)

	_, err := NewClient(t.Context(), discard(), &redis.Options{}, Binding{
		Role:    DefaultRole,
		Current: plain(current.Addr()),
		Next:    redisconn.PasswordEndpoint(next.Addr(), "wrong"),
		Tokens:  nil,
	})
	require.ErrorContains(t, err, "check next redis instance")
}

func TestStartFailsWhenTheNextInstanceRejectsTheToken(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	ca := redisconntest.NewCA(t)
	next := tlsServer(t, ca, nextToken)

	_, err := NewClient(t.Context(), discard(), &redis.Options{}, Binding{
		Role:    DefaultRole,
		Current: plain(current.Addr()),
		Next:    iamEndpoint(next.Addr(), ca),
		Tokens:  redisconntest.NewTokenSource("stale-token", time.Hour),
	})
	require.ErrorContains(t, err, "check next redis instance")
}

func TestArmedClientNeverRetriesAndCarriesNoCredentials(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	opts := f.client.Options()
	require.Equal(t, 0, opts.MaxRetries, "a command interrupted by the move must not be replayed")
	require.Equal(t, 1, opts.DialerRetries)
	require.Empty(t, opts.ClientName, "an instance that rejects CLIENT would refuse a named connection")
	require.Empty(t, opts.Username)
	require.Empty(t, opts.Password, "the dialer authenticates each socket; go-redis must send nothing")
	require.Nil(t, opts.CredentialsProvider)
	require.Nil(t, opts.CredentialsProviderContext)
	require.Nil(t, opts.StreamingCredentialsProvider)
	require.Nil(t, opts.TLSConfig)
}

func TestArmedClientDropsACallersConnectionName(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	next := miniredis.RunT(t)

	client, err := NewClient(t.Context(), discard(), &redis.Options{ClientName: "caller"}, binding(current.Addr(), next.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.Empty(t, client.Options().ClientName)
}

func TestCommandsUseTheCurrentInstanceByDefault(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	require.NoError(t, f.client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, f.current.Exists("k"))
	require.False(t, f.next.Exists("k"))
}

func TestHeldCommandRunsOnTheNextInstanceAfterTheMove(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	require.NoError(t, f.client.Set(t.Context(), "before", "v", 0).Err())
	f.hold(t)

	done := make(chan error, 1)
	go func() { done <- f.client.Set(context.WithoutCancel(t.Context()), "during", "v", 0).Err() }()

	f.move(t)
	require.NoError(t, <-done)

	require.False(t, f.current.Exists("during"), "a held command must never reach the instance being left")
	require.True(t, f.next.Exists("during"))

	require.NoError(t, f.client.Set(t.Context(), "after", "v", 0).Err())
	require.True(t, f.next.Exists("after"))
	require.False(t, f.current.Exists("after"))
}

func TestHeldCommandResumesOnTheCurrentInstanceWhenTheHoldEnds(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.hold(t)

	done := make(chan error, 1)
	go func() { done <- f.client.Set(context.WithoutCancel(t.Context()), "during", "v", 0).Err() }()

	// The operator's hold expires or is removed without a move.
	f.current.Del(HoldKey)
	f.eventually(t, stateCurrent)
	require.NoError(t, <-done)

	require.True(t, f.current.Exists("during"))
	require.False(t, f.next.Exists("during"))
}

func TestHeldCommandFailsWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.hold(t)

	err := f.client.Set(shortContext(t), "k", "v", 0).Err()
	require.ErrorIs(t, err, ErrHolding)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, f.current.Exists("k"))
}

func TestHeldPipelineAndTransactionWait(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.hold(t)
	ctx := shortContext(t)

	_, err := f.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, "piped", "v", 0)
		return nil
	})
	require.ErrorIs(t, err, ErrHolding)

	_, err = f.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, "transacted", "v", 0)
		return nil
	})
	require.ErrorIs(t, err, ErrHolding)

	require.False(t, f.current.Exists("piped"))
	require.False(t, f.current.Exists("transacted"))
}

func TestPingIsAnsweredDuringADeliberateHold(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.hold(t)

	require.NoError(t, f.client.Ping(shortContext(t)).Err())
}

func TestDialIsRefusedWhileHolding(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.hold(t)

	_, err := f.sw.dial(t.Context(), "tcp", "ignored")
	require.ErrorIs(t, err, ErrHolding)
}

func TestStaleReadingHoldsEvenThoughItSaidCurrent(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.eventually(t, stateCurrent)

	// The process stalls: its last reading ages without a newer one.
	f.current.Close()
	f.ahead.Store(int64(2 * staleAfter))

	got, _ := f.sw.load()
	require.Equal(t, stateUnknown, got)

	_, err := f.sw.dial(t.Context(), "tcp", "ignored")
	require.ErrorIs(t, err, ErrHolding, "a stalled process must not open a connection on an old reading")

	ctx := shortContext(t)
	require.ErrorIs(t, f.client.Set(ctx, "k", "v", 0).Err(), ErrHolding)
	require.ErrorIs(t, f.client.Ping(ctx).Err(), ErrHolding, "an unreadable switch is a fault and must fail health checks")
}

func TestMissingStateKeyHolds(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.current.Del(stateKey)
	f.eventually(t, stateUnknown)

	require.ErrorIs(t, f.client.Set(shortContext(t), "k", "v", 0).Err(), ErrHolding)
	require.False(t, f.current.Exists("k"))
}

func TestUnrecognisedStateValueHolds(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	require.NoError(t, f.current.Set(stateKey, "nonsense"))
	f.eventually(t, stateUnknown)

	require.ErrorIs(t, f.client.Set(shortContext(t), "k", "v", 0).Err(), ErrHolding)
}

func TestMoveIsFinal(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.move(t)

	// Nothing that happens to the instance that was left can move the process back.
	require.NoError(t, f.current.Set(stateKey, valueCurrent))
	f.current.Close()
	f.ahead.Store(int64(time.Hour))

	require.NoError(t, f.client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, f.next.Exists("k"))
}

func TestStartingDuringAHoldWaits(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	require.NoError(t, current.Set(HoldKey, "run-1"))
	next := miniredis.RunT(t)

	client, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	ctx := shortContext(t)
	require.NoError(t, client.Ping(ctx).Err(), "startup pings must pass during a deliberate hold")
	require.ErrorIs(t, client.Set(ctx, "k", "v", 0).Err(), ErrHolding)
}

func TestStartingAfterTheMoveUsesTheNextInstance(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, movedPrefix+"run-1"))
	next := miniredis.RunT(t)

	client, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, next.Exists("k"))
	require.False(t, current.Exists("k"))
}

func TestWrapGivesACopiedClientTheSameGate(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	next := miniredis.RunT(t)

	first, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	copied := *first.Options()
	second := Wrap(&copied)
	t.Cleanup(func() { _ = second.Close() })
	require.Equal(t, 0, second.Options().MaxRetries, "copied options would otherwise restore the default retries")

	require.NoError(t, current.Set(HoldKey, "run-1"))
	require.Eventually(t, func() bool {
		return second.Set(shortContext(t), "probe", "v", 0).Err() != nil
	}, settle, 20*time.Millisecond)

	require.ErrorIs(t, second.Set(shortContext(t), "k", "v", 0).Err(), ErrHolding)
	require.False(t, current.Exists("k"), "the copied client must hold too")
}

func TestWrapLeavesOtherOptionsAlone(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := Wrap(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.Equal(t, 3, client.Options().MaxRetries)
}

func TestNewConnectionIsRefusedWhenTheInstanceItselfSaysHold(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	// The hold is set on the instance, but this process has not polled since:
	// its cached reading still says current.
	require.NoError(t, f.current.Set(HoldKey, "run-1"))

	conn, err := net.Dial("tcp", f.current.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	require.ErrorIs(t, f.sw.verifyCurrent(t.Context(), conn), ErrHolding)
}

func TestNewConnectionIsAcceptedWhenTheInstanceSaysCurrent(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	conn, err := net.Dial("tcp", f.current.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	require.NoError(t, f.sw.verifyCurrent(t.Context(), conn))

	// The socket is still open, and already authenticated: its real owner
	// needs no password.
	owner := redis.NewClient(&redis.Options{
		Dialer: func(context.Context, string, string) (net.Conn, error) { return conn, nil },
	})
	t.Cleanup(func() { _ = owner.Close() })
	require.NoError(t, owner.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, f.current.Exists("k"))
}

func TestNewConnectionIsRefusedWhenTheInstanceHasMoved(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	// The move is recorded on the instance, but this process has not polled
	// since: a socket whose handshake was delayed must still be refused.
	require.NoError(t, f.current.Set(stateKey, movedPrefix+"run-1"))

	conn, err := net.Dial("tcp", f.current.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	require.ErrorIs(t, f.sw.verifyCurrent(t.Context(), conn), ErrHolding)
}

func TestWrapBindsTheDialerEvenForOptionsThatOnlyShareTheAddress(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, movedPrefix+"run-1"))
	next := miniredis.RunT(t)

	first, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	// Options built by hand, with the right address but no dialer.
	second := Wrap(&redis.Options{Addr: current.Addr()})
	t.Cleanup(func() { _ = second.Close() })

	require.NoError(t, second.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, next.Exists("k"), "a gated client must also follow the switch to the next instance")
	require.False(t, current.Exists("k"))
}

func TestWrapWaitsForASwitchThatIsStillStarting(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, movedPrefix+"run-1"))
	next := miniredis.RunT(t)

	// The registry as NewClient leaves it between reserving and starting.
	key := registryKey{addr: current.Addr(), role: DefaultRole}
	reserved := &entry{ready: make(chan struct{}), sw: nil}
	switches.Store(key, reserved)
	t.Cleanup(func() { switches.Delete(key) })

	wrapped := make(chan *redis.Client)
	go func() {
		wrapped <- Wrap(&redis.Options{Addr: current.Addr()})
	}()

	select {
	case <-wrapped:
		require.Fail(t, "Wrap returned a client before the switch had started")
	case <-time.After(100 * time.Millisecond):
	}

	sw, err := start(t.Context(), discard(), DefaultRole, connector(t, plain(current.Addr()), nil), connector(t, plain(next.Addr()), nil), 0)
	require.NoError(t, err)
	reserved.sw = sw
	close(reserved.ready)

	client := <-wrapped
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, next.Exists("k"), "the client built during startup must follow the switch")
	require.False(t, current.Exists("k"))
}

func TestWrapRefusesToConnectForASwitchThatFailedToStart(t *testing.T) {
	t.Parallel()

	// No state key: the switch cannot start.
	current := miniredis.RunT(t)
	next := miniredis.RunT(t)

	_, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.ErrorIs(t, err, ErrNotArmed)

	client := Wrap(&redis.Options{Addr: current.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	require.ErrorIs(t, client.Set(t.Context(), "k", "v", 0).Err(), ErrNotStarted)
	require.False(t, current.Exists("k"), "a client whose switch failed to start must not connect")

	// Starting again once the instance is armed replaces the failed entry.
	require.NoError(t, current.Set(stateKey, valueCurrent))
	retried, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = retried.Close() })
	require.NoError(t, retried.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, current.Exists("k"))
}

func TestMoveLeavesNoConnectionToTheInstanceBeingLeft(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	require.NoError(t, f.client.Set(t.Context(), "before", "v", 0).Err())

	f.sw.mu.Lock()
	open := len(f.sw.conns)
	f.sw.mu.Unlock()
	require.Positive(t, open, "the fixture's command should have opened a tracked connection")

	f.move(t)

	f.sw.mu.Lock()
	remaining := len(f.sw.conns)
	f.sw.mu.Unlock()
	require.Zero(t, remaining)
}

func TestWaitersAllWakeWhenTheMoveHappens(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.hold(t)

	const waiters = 50
	done := make(chan error, waiters)
	for range waiters {
		go func() { done <- f.sw.wait(context.WithoutCancel(t.Context())) }()
	}

	f.move(t)
	for range waiters {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(settle):
			require.FailNow(t, "a waiter missed the move and is still waiting")
		}
	}
}

func TestNewClientRefusesASecondSwitchForTheSameInstanceAndRole(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	next := miniredis.RunT(t)

	first, err := NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	_, err = NewClient(t.Context(), discard(), &redis.Options{}, binding(current.Addr(), next.Addr()))
	require.ErrorContains(t, err, "already follows role")
}

// recordingConn records every byte written to it.
type recordingConn struct {
	net.Conn
	mu      *sync.Mutex
	written *bytes.Buffer
}

func (c recordingConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.written.Write(p)
	c.mu.Unlock()
	n, err := c.Conn.Write(p)
	return n, err //nolint:wrapcheck // test double: the socket's own error is the signal
}

func TestDialStalledAcrossAMoveNeverSendsTheNextCredentials(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	current := miniredis.RunT(t)
	current.RequireAuth(currentPassword)
	next := tlsServer(t, ca, nextToken)
	tokens := redisconntest.NewTokenSource(nextToken, time.Hour)

	f := newSwitchFixture(t, current, next,
		redisconn.PasswordEndpoint(current.Addr(), currentPassword),
		iamEndpoint(next.Addr(), ca),
		tokens,
		&redis.Options{DialTimeout: time.Second})
	f.eventually(t, stateCurrent)

	// The dial chooses the current instance, then stalls opening its socket
	// until the move has landed. Everything it then writes is recorded.
	var mu sync.Mutex
	var written bytes.Buffer
	stalled, release := make(chan struct{}), make(chan struct{})
	releaseDial := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseDial)
	f.sw.dialTCP = func(ctx context.Context, network, addr string, timeout time.Duration) (net.Conn, error) {
		close(stalled)
		<-release
		conn, err := dialTCP(ctx, network, addr, timeout)
		if err != nil {
			return nil, err
		}
		return recordingConn{Conn: conn, mu: &mu, written: &written}, nil
	}

	result := make(chan error, 1)
	go func() {
		conn, err := f.sw.dial(context.WithoutCancel(t.Context()), "tcp", "ignored")
		if conn != nil {
			_ = conn.Close()
		}
		result <- err
	}()

	select {
	case <-stalled:
	case <-time.After(5 * time.Second):
		t.Fatal("dial did not reach the stalled socket")
	}
	f.move(t)
	releaseDial()

	require.ErrorIs(t, <-result, ErrHolding, "the instance that was left refuses the socket")

	mu.Lock()
	sent := written.String()
	mu.Unlock()
	require.Contains(t, sent, currentPassword, "the socket authenticated as the instance it reached")
	require.NotContains(t, sent, nextToken, "the next instance's token must never reach the instance that was left")
	require.Equal(t, 1, tokens.Served(), "only the startup check of the next instance asked for a token")
}

func TestTLSCurrentReusesPooledConnections(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	current := tlsServer(t, ca, "current-token")
	next := tlsServer(t, ca, "current-token")
	tokens := redisconntest.NewTokenSource("current-token", time.Hour)

	f := newSwitchFixture(t, current, next,
		iamEndpoint(current.Addr(), ca),
		iamEndpoint(next.Addr(), ca),
		tokens,
		&redis.Options{DialTimeout: time.Second, PoolSize: 1})

	for range 5 {
		require.NoError(t, f.client.Set(t.Context(), "k", "v", 0).Err())
	}
	require.True(t, current.Exists("k"))
	require.Equal(t, 1, f.dials.to(current.Addr()), "a pooled TLS connection to the current instance must be reused, not discarded as unhealthy")
}

func TestTLSNextAfterTheMoveReusesPooledConnections(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	current := miniredis.RunT(t)
	current.RequireAuth(currentPassword)
	next := tlsServer(t, ca, nextToken)

	f := newSwitchFixture(t, current, next,
		redisconn.PasswordEndpoint(current.Addr(), currentPassword),
		iamEndpoint(next.Addr(), ca),
		redisconntest.NewTokenSource(nextToken, time.Hour),
		&redis.Options{DialTimeout: time.Second, PoolSize: 1})

	require.NoError(t, f.client.Set(t.Context(), "before", "v", 0).Err())
	f.move(t)

	for range 5 {
		require.NoError(t, f.client.Set(t.Context(), "after", "v", 0).Err())
	}
	require.True(t, next.Exists("after"))
	require.False(t, current.Exists("after"))
	require.Equal(t, 1, f.dials.to(next.Addr()))
}

func TestTLSCurrentMovesToTLSNext(t *testing.T) {
	t.Parallel()

	ca := redisconntest.NewCA(t)
	current := tlsServer(t, ca, "current-token")
	next := tlsServer(t, ca, "current-token")

	f := newSwitchFixture(t, current, next,
		iamEndpoint(current.Addr(), ca),
		iamEndpoint(next.Addr(), ca),
		redisconntest.NewTokenSource("current-token", time.Hour),
		&redis.Options{DialTimeout: time.Second})

	require.NoError(t, f.client.Set(t.Context(), "before", "v", 0).Err())
	f.hold(t)
	f.move(t)
	require.NoError(t, f.client.Set(t.Context(), "after", "v", 0).Err())

	require.True(t, current.Exists("before"))
	require.True(t, next.Exists("after"))
	require.False(t, current.Exists("after"))
}

func TestTwoRolesOnOneInstanceMoveIndependently(t *testing.T) {
	t.Parallel()

	source := miniredis.RunT(t)
	require.NoError(t, source.Set(StateKey(RoleAuth), valueCurrent))
	require.NoError(t, source.Set(StateKey(RoleOps), valueCurrent))
	authNext, opsNext := miniredis.RunT(t), miniredis.RunT(t)

	auth, err := NewClient(t.Context(), discard(), &redis.Options{}, Binding{Role: RoleAuth, Current: plain(source.Addr()), Next: plain(authNext.Addr()), Tokens: nil})
	require.NoError(t, err)
	t.Cleanup(func() { _ = auth.Close() })
	ops, err := NewClient(t.Context(), discard(), &redis.Options{}, Binding{Role: RoleOps, Current: plain(source.Addr()), Next: plain(opsNext.Addr()), Tokens: nil})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ops.Close() })

	// The hold is shared: both roles wait.
	require.NoError(t, source.Set(HoldKey, "run-1"))
	require.Eventually(t, func() bool {
		return auth.Set(shortContext(t), "probe", "v", 0).Err() != nil && ops.Set(shortContext(t), "probe", "v", 0).Err() != nil
	}, settle, 20*time.Millisecond)

	// Only the auth role moves.
	require.NoError(t, source.Set(StateKey(RoleAuth), movedPrefix+"run-1"))
	source.Del(HoldKey)

	require.Eventually(t, func() bool {
		return auth.Set(shortContext(t), "auth-key", "v", 0).Err() == nil
	}, settle, 20*time.Millisecond)
	require.NoError(t, ops.Set(t.Context(), "ops-key", "v", 0).Err())

	require.True(t, authNext.Exists("auth-key"))
	require.False(t, source.Exists("auth-key"))
	require.True(t, source.Exists("ops-key"))
	require.False(t, opsNext.Exists("ops-key"))
	require.False(t, authNext.Exists("ops-key"))

	// Then the ops role moves to its own next instance.
	require.NoError(t, source.Set(StateKey(RoleOps), movedPrefix+"run-2"))
	require.Eventually(t, func() bool {
		return ops.Set(shortContext(t), "ops-after", "v", 0).Err() == nil && opsNext.Exists("ops-after")
	}, settle, 20*time.Millisecond)
	require.False(t, authNext.Exists("ops-after"))

	// A copied client of either role needs the role named.
	copied := *ops.Options()
	ambiguous := Wrap(&copied)
	t.Cleanup(func() { _ = ambiguous.Close() })
	require.ErrorIs(t, ambiguous.Ping(t.Context()).Err(), ErrAmbiguousRole)

	unknown := WrapRole(&copied, "unknown")
	t.Cleanup(func() { _ = unknown.Close() })
	require.ErrorIs(t, unknown.Ping(t.Context()).Err(), ErrUnknownRole)

	named := WrapRole(&copied, RoleOps)
	t.Cleanup(func() { _ = named.Close() })
	require.NoError(t, named.Set(t.Context(), "ops-copied", "v", 0).Err())
	require.True(t, opsNext.Exists("ops-copied"))
}

func TestDialsRightAfterAMoveAreSpread(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	var waits atomic.Int64
	f.sw.mu.Lock()
	f.sw.jitter = func() time.Duration {
		waits.Add(1)
		return time.Millisecond
	}
	f.sw.mu.Unlock()

	// Before the move, dials to the current instance are not delayed.
	conn, err := f.sw.dial(t.Context(), "tcp", "ignored")
	require.NoError(t, err)
	_ = conn.Close()
	require.Zero(t, waits.Load())

	f.move(t)
	conn, err = f.sw.dial(t.Context(), "tcp", "ignored")
	require.NoError(t, err)
	_ = conn.Close()
	require.Equal(t, int64(1), waits.Load(), "a dial soon after the move waits a random moment")

	f.ahead.Store(int64(2 * moveJitterWindow))
	conn, err = f.sw.dial(t.Context(), "tcp", "ignored")
	require.NoError(t, err)
	_ = conn.Close()
	require.Equal(t, int64(1), waits.Load(), "dials long after the move are not delayed")
}

func TestTrackedPlainSocketExposesItsRawSocket(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	tcp, err := net.Dial("tcp", f.current.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tcp.Close() })
	_, handed := f.sw.track(tcp)
	sc, ok := handed.(syscall.Conn)
	require.True(t, ok, "a plain socket keeps the pool's liveness check")
	raw, err := sc.SyscallConn()
	require.NoError(t, err)

	called := false
	require.NoError(t, raw.Control(func(uintptr) { called = true }))
	require.True(t, called, "the check reaches the real socket")
}

func TestTrackedConnWithoutARawSocketFailsTheCheckOnlyOnceClosed(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	left, right := net.Pipe()
	t.Cleanup(func() { _ = right.Close() })
	tracked, handed := f.sw.track(left)
	sc, ok := handed.(syscall.Conn)
	require.True(t, ok)
	raw, err := sc.SyscallConn()
	require.NoError(t, err)

	touched := false
	require.NoError(t, raw.Read(func(uintptr) bool { touched = true; return true }), "an open connection passes")
	require.False(t, touched, "the stand-in never reads the socket, which would consume TLS records")

	// As the move does.
	require.NoError(t, tracked.closeSocket())
	require.ErrorIs(t, raw.Read(func(uintptr) bool { return true }), net.ErrClosed, "a closed connection is dropped by the pool")
}
