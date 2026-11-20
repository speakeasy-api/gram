package aivendors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// lockedBuffer lets the refresh goroutine log while the test reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write log: %w", err)
	}
	return n, nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRefreshLoopPublishesOnNextMinuteAndStops(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		initial := NewSnapshot(Products())
		store := NewStore(initial)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		var calls atomic.Int32
		logs := &lockedBuffer{}
		logger := slog.New(slog.NewTextHandler(logs, nil))
		products := Products()
		products[0].DisplayName = "Updated name"
		next := NewSnapshot(products)
		go func() {
			defer close(done)
			store.run(ctx, logger, noop.Int64Counter{}, func(context.Context) error {
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
		require.Contains(t, logs.String(), "keeping the current snapshot")
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

func TestRefreshLoopSurvivesPanicsAndLogsEachFailureOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		initial := NewSnapshot(Products())
		store := NewStore(initial)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		var calls atomic.Int32
		logs := &lockedBuffer{}
		logger := slog.New(slog.NewTextHandler(logs, nil))
		go func() {
			defer close(done)
			store.run(ctx, logger, noop.Int64Counter{}, func(context.Context) error {
				switch calls.Add(1) {
				case 1:
					panic("fixture panic")
				case 2, 3:
					return errors.New("bad row")
				default:
					return nil
				}
			})
		}()
		for range 4 {
			synctest.Wait()
			<-time.After(RefreshInterval)
		}
		synctest.Wait()
		cancel()
		<-done
		require.Equal(t, int32(4), calls.Load())
		require.Same(t, initial, store.Snapshot())
		out := logs.String()
		require.Equal(t, 1, strings.Count(out, "fixture panic"))
		require.Contains(t, out, "refreshRecovered", "the panic log carries its stack")
		require.Equal(t, 1, strings.Count(out, "bad row"), "a repeated failure is logged once")
		require.Contains(t, out, "AI catalog refresh recovered")
	})
}

func TestRefreshMetricsMove(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		store := NewStore(CompiledSnapshot())
		logger := slog.New(slog.NewTextHandler(&lockedBuffer{}, nil))
		reader := sdkmetric.NewManualReader()
		failures, stop := store.refreshMetrics(logger, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
		defer stop()
		read := func() (int64, float64, int64) {
			var collected metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(context.Background(), &collected))
			var count, builtIn int64
			var age float64
			for _, scope := range collected.ScopeMetrics {
				for _, m := range scope.Metrics {
					switch data := m.Data.(type) {
					case metricdata.Sum[int64]:
						if m.Name == meterRefreshFailures {
							count = data.DataPoints[0].Value
						}
					case metricdata.Gauge[float64]:
						age = data.DataPoints[0].Value
					case metricdata.Gauge[int64]:
						builtIn = data.DataPoints[0].Value
					}
				}
			}
			return count, age, builtIn
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		var calls atomic.Int32
		next := NewSnapshot(Products())
		go func() {
			defer close(done)
			store.run(ctx, logger, failures, func(context.Context) error {
				if n := calls.Add(1); n != 3 {
					return errors.New("database unavailable")
				}
				store.confirmed.Store(time.Now().UnixNano())
				store.mu.Lock()
				store.snapshot = next
				store.mu.Unlock()
				return nil
			})
		}()
		synctest.Wait()
		<-time.After(2 * RefreshInterval)
		synctest.Wait()
		count, age, builtIn := read()
		require.Equal(t, int64(2), count)
		require.Zero(t, age, "no age is reported while the built-in catalog is served")
		require.Equal(t, int64(1), builtIn)
		<-time.After(RefreshInterval)
		synctest.Wait()
		count, age, builtIn = read()
		require.Equal(t, int64(2), count)
		require.InDelta(t, 0, age, 1)
		require.Equal(t, int64(0), builtIn)
		<-time.After(2 * RefreshInterval)
		synctest.Wait()
		count, age, builtIn = read()
		require.Equal(t, int64(4), count)
		require.InDelta(t, (2 * RefreshInterval).Seconds(), age, 1)
		require.Equal(t, int64(0), builtIn)
		cancel()
		<-done
	})
}
