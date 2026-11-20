package aivendors

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRefreshLoopPublishesOnNextMinuteAndStops(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		initial := NewSnapshot(Products())
		store := NewStore(initial)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		var calls atomic.Int32
		var logs lockedBuffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		products := Products()
		products[0].DisplayName = "Updated name"
		next := NewSnapshot(products)
		go func() {
			defer close(done)
			store.run(ctx, logger, func(context.Context) error {
				if calls.Add(1) == 1 {
					return errors.New("database unavailable")
				}
				store.mu.Lock()
				store.snapshot = next
				store.mu.Unlock()
				return nil
			})
		}()
		synctest.Wait()
		<-time.After(RefreshInterval)
		synctest.Wait()
		require.Contains(t, logs.String(), "retaining last snapshot")
		require.Equal(t, int32(1), calls.Load())
		require.Same(t, initial, store.Snapshot())
		<-time.After(RefreshInterval)
		synctest.Wait()
		require.Equal(t, int32(2), calls.Load())
		require.Same(t, next, store.Snapshot())
		cancel()
		<-done
	})
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
