package shutdowndrain_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shutdowndrain"
)

// testInterruptAfter is the interrupt deadline the tests arm. Inside a
// synctest bubble the clock is fake, so the value only orders events.
const testInterruptAfter = 45 * time.Second

func TestDrain_NilNeverStarts(t *testing.T) {
	t.Parallel()

	var drain *shutdowndrain.Drain
	drain.Start(testInterruptAfter)
	require.False(t, drain.Started())

	ctx, release := drain.Interruptible(t.Context())
	defer release()
	require.Equal(t, t.Context(), ctx, "a nil drain must not wrap the context")

	drain.AfterStart(func() { t.Error("AfterStart ran on a nil drain") })()
	drain.AfterInterrupt(func() { t.Error("AfterInterrupt ran on a nil drain") })()
}

func TestDrain_StartIsIdempotent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		drain := shutdowndrain.New()
		require.False(t, drain.Started())

		drain.Start(testInterruptAfter)
		require.True(t, drain.Started())

		// A later Start must not re-arm the interrupt deadline further out.
		time.Sleep(testInterruptAfter / 2) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		drain.Start(testInterruptAfter)

		ctx, release := drain.Interruptible(t.Context())
		defer release()

		time.Sleep(testInterruptAfter/2 + time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.True(t, shutdowndrain.Interrupted(ctx))
	})
}

func TestDrain_InterruptibleCancelsAtDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		drain := shutdowndrain.New()
		ctx, release := drain.Interruptible(t.Context())
		defer release()

		drain.Start(testInterruptAfter)
		time.Sleep(testInterruptAfter - time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.NoError(t, ctx.Err(), "the context must stay live until the interrupt deadline")

		time.Sleep(2 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.ErrorIs(t, context.Cause(ctx), shutdowndrain.ErrInterrupted)
		require.True(t, shutdowndrain.Interrupted(ctx))
	})
}

func TestDrain_InterruptibleAfterDeadlineIsCancelled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		drain := shutdowndrain.New()
		drain.Start(testInterruptAfter)
		time.Sleep(testInterruptAfter + time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()

		ctx, release := drain.Interruptible(t.Context())
		defer release()

		synctest.Wait()
		require.True(t, shutdowndrain.Interrupted(ctx))
	})
}

func TestDrain_ReleaseIsNotAnInterrupt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		drain := shutdowndrain.New()
		ctx, release := drain.Interruptible(t.Context())
		release()

		require.Error(t, ctx.Err(), "release must cancel the context")
		require.False(t, shutdowndrain.Interrupted(ctx))

		drain.Start(testInterruptAfter)
		time.Sleep(testInterruptAfter + time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.False(t, shutdowndrain.Interrupted(ctx), "a released context must not be interrupted later")
	})
}

func TestDrain_ParentCancellationIsNotAnInterrupt(t *testing.T) {
	t.Parallel()

	drain := shutdowndrain.New()
	parent, cancelParent := context.WithCancel(t.Context())
	ctx, release := drain.Interruptible(parent)
	defer release()

	cancelParent()
	require.Error(t, ctx.Err())
	require.False(t, shutdowndrain.Interrupted(ctx))
}

func TestDrain_AfterCallbacksRunInOrder(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		drain := shutdowndrain.New()
		started := make(chan struct{})
		interrupted := make(chan struct{})
		defer drain.AfterStart(func() { close(started) })()
		defer drain.AfterInterrupt(func() { close(interrupted) })()

		drain.Start(testInterruptAfter)
		synctest.Wait()
		requireClosed(t, started, "AfterStart callback")
		requireOpen(t, interrupted, "AfterInterrupt callback")

		time.Sleep(testInterruptAfter + time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		requireClosed(t, interrupted, "AfterInterrupt callback")
	})
}

func TestDrain_StoppedCallbacksDoNotRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		drain := shutdowndrain.New()
		drain.AfterStart(func() { t.Error("stopped AfterStart callback ran") })()
		drain.AfterInterrupt(func() { t.Error("stopped AfterInterrupt callback ran") })()

		drain.Start(testInterruptAfter)
		time.Sleep(testInterruptAfter + time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
	})
}

func requireClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	default:
		t.Fatalf("%s did not run", what)
	}
}

func requireOpen(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
		t.Fatalf("%s ran too early", what)
	default:
	}
}
