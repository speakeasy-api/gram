package redisswitch

import (
	"context"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const (
	currentPassword = "current-password"
	nextPassword    = "next-password"

	// stateKey is the state key of the only role these tests use.
	stateKey = stateKeyPrefix + DefaultRole

	// settle is how long a test waits for the poller to observe a change.
	settle = 5 * time.Second
)

type fixture struct {
	current *miniredis.Miniredis
	next    *miniredis.Miniredis
	sw      *Switch
	client  *redis.Client

	// ahead is added to the switch's clock, to age its last reading.
	ahead atomic.Int64
}

// newFixture starts two password-protected instances, arms the first, and
// returns a client that follows its switch keys. Different passwords prove the
// client authenticates to whichever instance it is using.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	f := &fixture{current: miniredis.RunT(t), next: miniredis.RunT(t), sw: nil, client: nil, ahead: atomic.Int64{}}
	f.current.RequireAuth(currentPassword)
	f.next.RequireAuth(nextPassword)
	require.NoError(t, f.current.Set(stateKey, valueCurrent))

	opts := &redis.Options{Addr: f.current.Addr(), Password: currentPassword, DialTimeout: time.Second}
	sw, err := start(t.Context(), slog.New(slog.DiscardHandler), DefaultRole,
		Target{Addr: opts.Addr, Password: opts.Password},
		Target{Addr: f.next.Addr(), Password: nextPassword},
		opts.DialTimeout)
	require.NoError(t, err)
	sw.mu.Lock()
	sw.now = func() time.Time { return time.Now().Add(time.Duration(f.ahead.Load())) }
	sw.mu.Unlock()

	f.sw = sw
	f.client = sw.client(opts)
	t.Cleanup(func() { _ = f.client.Close() })

	return f
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
	client, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: server.Addr()}, DefaultRole, Target{Addr: "", Password: ""})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, server.Exists("k"))
	require.Equal(t, 3, client.Options().MaxRetries, "an unarmed client keeps the default retries")
}

func TestNewClientWithNextEqualToCurrentIsAnOrdinaryClient(t *testing.T) {
	t.Parallel()

	// No state key: an armed client would refuse to start.
	server := miniredis.RunT(t)
	client, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: server.Addr()}, DefaultRole, Target{Addr: server.Addr(), Password: ""})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Set(t.Context(), "k", "v", 0).Err())
	require.True(t, server.Exists("k"))
	require.Equal(t, 3, client.Options().MaxRetries, "an unarmed client keeps the default retries")
}

func TestStartRefusesAnUnarmedInstance(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	next := miniredis.RunT(t)

	_, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
	require.ErrorIs(t, err, ErrNotArmed)
}

func TestStartFailsWhenTheKeysCannotBeRead(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	addr := server.Addr()
	server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	_, err := NewClient(ctx, slog.New(slog.DiscardHandler), &redis.Options{Addr: addr}, DefaultRole, Target{Addr: "127.0.0.1:1", Password: ""})
	require.Error(t, err)
}

func TestStartFailsWhenTheNextInstanceRejectsThePassword(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	next := miniredis.RunT(t)
	next.RequireAuth(nextPassword)

	_, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: "wrong"})
	require.ErrorContains(t, err, "check next redis instance")
}

func TestArmedClientNeverRetriesAndDoesNotNameItsConnections(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	require.Equal(t, 0, f.client.Options().MaxRetries, "a command interrupted by the move must not be replayed")
	require.Equal(t, 1, f.client.Options().DialerRetries)
	require.Empty(t, f.client.Options().ClientName, "an instance that rejects CLIENT would refuse a named connection")
}

func TestArmedClientDropsACallersConnectionName(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	next := miniredis.RunT(t)

	client, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr(), ClientName: "caller"}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
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

	client, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
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

	client, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
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

	first, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
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

	// The socket is still open and usable by its real owner afterwards.
	owner := redis.NewClient(&redis.Options{
		Dialer:   func(context.Context, string, string) (net.Conn, error) { return conn, nil },
		Password: currentPassword,
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

	first, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
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
	key := current.Addr()
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

	sw, err := start(t.Context(), slog.New(slog.DiscardHandler), DefaultRole, Target{Addr: current.Addr(), Password: ""}, Target{Addr: next.Addr(), Password: ""}, 0)
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

	_, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
	require.ErrorIs(t, err, ErrNotArmed)

	client := Wrap(&redis.Options{Addr: current.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	require.ErrorIs(t, client.Set(t.Context(), "k", "v", 0).Err(), ErrNotStarted)
	require.False(t, current.Exists("k"), "a client whose switch failed to start must not connect")

	// Starting again once the instance is armed replaces the failed entry.
	require.NoError(t, current.Set(stateKey, valueCurrent))
	retried, err := NewClient(t.Context(), slog.New(slog.DiscardHandler), &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
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

func TestNewClientRefusesASecondSwitchForTheSameInstance(t *testing.T) {
	t.Parallel()

	current := miniredis.RunT(t)
	require.NoError(t, current.Set(stateKey, valueCurrent))
	next := miniredis.RunT(t)
	logger := slog.New(slog.DiscardHandler)

	first, err := NewClient(t.Context(), logger, &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	_, err = NewClient(t.Context(), logger, &redis.Options{Addr: current.Addr()}, DefaultRole, Target{Addr: next.Addr(), Password: ""})
	require.ErrorContains(t, err, "already follows this instance")
}
