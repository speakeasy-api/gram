package aivendors

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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
		calls := 0
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))
		products := Products()
		products[0].DisplayName = "Updated name"
		next := NewSnapshot(products)
		go func() {
			defer close(done)
			store.run(ctx, logger, func(context.Context) error {
				calls++
				if calls == 1 {
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
		require.Equal(t, 1, calls)
		require.Same(t, initial, store.Snapshot())
		<-time.After(RefreshInterval)
		synctest.Wait()
		require.Equal(t, 2, calls)
		require.Same(t, next, store.Snapshot())
		cancel()
		<-done
	})
}
