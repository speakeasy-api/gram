package risk_analysis

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPeriodicHeartbeatContinuesDuringLongRunningWorkAndStops(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var beats atomic.Int64
		stop := startPeriodicHeartbeat(t.Context(), time.Second, func() {
			beats.Add(1)
		})

		require.Equal(t, int64(1), beats.Load(), "activity starts with an immediate heartbeat")
		time.Sleep(3 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.Equal(t, int64(4), beats.Load(), "long-running work keeps heartbeating once per interval")

		stop()
		stoppedAt := beats.Load()
		time.Sleep(2 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.Equal(t, stoppedAt, beats.Load(), "activity completion terminates heartbeat work")
	})
}

func TestPeriodicHeartbeatStopsWhenActivityContextIsCanceled(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		var beats atomic.Int64
		stop := startPeriodicHeartbeat(ctx, time.Second, func() {
			beats.Add(1)
		})

		time.Sleep(time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.Equal(t, int64(2), beats.Load())

		cancel()
		synctest.Wait()
		canceledAt := beats.Load()
		time.Sleep(2 * time.Second) //nolint:forbidigo // GG013: advances the synctest fake clock instantly (the only way to move past a timer inside a synctest.Test bubble); this exemption is valid ONLY within synctest.Test — do not copy it to a real-time time.Sleep
		synctest.Wait()
		require.Equal(t, canceledAt, beats.Load(), "activity cancellation terminates heartbeat work")
		stop()
	})
}
