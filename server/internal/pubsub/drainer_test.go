package pubsub_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/pubsub"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	// Mirrors the unexported drainWorkers and queueDepth in the package under
	// test. The overflow test needs them to enqueue past the queue's capacity.
	testDrainWorkers = 8
	testQueueDepth   = 4096

	testComponent = "test_publisher"
	testLogMsg    = "failed to publish to pubsub"
)

// resolvedResult is a PublishResult that is ready the moment it is created.
type resolvedResult struct {
	err error
}

func (r *resolvedResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (r *resolvedResult) Get(context.Context) (string, error) {
	return "", r.err
}

// blockingResult resolves only once release is closed, so tests can hold the
// drain pool at a known point and observe the queue filling behind it.
type blockingResult struct {
	release <-chan struct{}
}

func (r *blockingResult) Ready() <-chan struct{} {
	return r.release
}

func (r *blockingResult) Get(ctx context.Context) (string, error) {
	select {
	case <-r.release:
		return "", nil
	case <-ctx.Done():
		//nolint:wrapcheck // test fake mirroring the real client's behaviour
		return "", ctx.Err()
	}
}

func newTestDrainer(t *testing.T, logger *slog.Logger) (*pubsub.Drainer, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	drainer := pubsub.NewDrainer(
		logger,
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		testComponent,
		testLogMsg,
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = drainer.Close(ctx)
	})

	return drainer, reader
}

// counterValue reads the point of an int64 counter carrying the given
// component attribute, or 0 when the counter never recorded for it.
func counterValue(t *testing.T, reader *sdkmetric.ManualReader, name string, component string) int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %s must be an int64 sum", name)

			for _, dp := range sum.DataPoints {
				if v, found := dp.Attributes.Value(attr.ComponentKey); found && v.AsString() == component {
					return dp.Value
				}
			}
		}
	}

	return 0
}

// TestDrainer_ObservesSuccessfulPublishes verifies the happy path: results are
// resolved and neither counter moves.
func TestDrainer_ObservesSuccessfulPublishes(t *testing.T) {
	t.Parallel()

	drainer, reader := newTestDrainer(t, testenv.NewLogger(t))

	for range 10 {
		drainer.Enqueue(t.Context(), gcp.NewSuccessPublishResult())
	}
	drainer.Wait()

	require.Zero(t, counterValue(t, reader, "pubsub.publish.ack_failures", testComponent))
	require.Zero(t, counterValue(t, reader, "pubsub.publish.ack_drops", testComponent))
}

// TestDrainer_CountsAckFailures verifies that a failing ack increments the
// failure counter per message rather than per batch.
func TestDrainer_CountsAckFailures(t *testing.T) {
	t.Parallel()

	drainer, reader := newTestDrainer(t, testenv.NewLogger(t))

	failing := &resolvedResult{err: errors.New("broker unavailable")}
	drainer.Enqueue(t.Context(), failing, failing, gcp.NewSuccessPublishResult())
	drainer.Wait()

	require.Equal(t, int64(2), counterValue(t, reader, "pubsub.publish.ack_failures", testComponent))
	require.Zero(t, counterValue(t, reader, "pubsub.publish.ack_drops", testComponent))
}

// TestDrainer_DropsWhenQueueFull is the property the pool exists for: enqueue
// never blocks the publishing caller, and results that cannot be queued are
// counted rather than silently discarded or queued without limit.
func TestDrainer_DropsWhenQueueFull(t *testing.T) {
	t.Parallel()

	drainer, reader := newTestDrainer(t, testenv.NewLogger(t))

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	blocked := &blockingResult{release: release}

	// Fill the queue, the workers, and then some. Each worker holds one result
	// outside the queue, so between 50 and 50+testDrainWorkers of the extras
	// are guaranteed to find no room.
	const extras = 50
	for range testQueueDepth + testDrainWorkers + extras {
		drainer.Enqueue(t.Context(), blocked)
	}

	dropped := counterValue(t, reader, "pubsub.publish.ack_drops", testComponent)
	require.GreaterOrEqual(t, dropped, int64(extras))
	require.LessOrEqual(t, dropped, int64(extras+testDrainWorkers))
}

// TestDrainer_EnqueueAfterCloseDrops verifies a publish racing shutdown is
// accounted for instead of panicking on a closed queue.
func TestDrainer_EnqueueAfterCloseDrops(t *testing.T) {
	t.Parallel()

	drainer, reader := newTestDrainer(t, testenv.NewLogger(t))

	require.NoError(t, drainer.Close(t.Context()))
	drainer.Enqueue(t.Context(), gcp.NewSuccessPublishResult())

	require.Equal(t, int64(1), counterValue(t, reader, "pubsub.publish.ack_drops", testComponent))
}

// TestDrainer_CloseIsIdempotent verifies shutdown paths can call Close twice
// without closing an already-closed queue.
func TestDrainer_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	drainer, _ := newTestDrainer(t, testenv.NewLogger(t))

	drainer.Enqueue(t.Context(), gcp.NewSuccessPublishResult())
	require.NoError(t, drainer.Close(t.Context()))
	require.NoError(t, drainer.Close(t.Context()))
}

// TestDrainer_CloseDrainsQueuedResults verifies Close does not abandon results
// that are already queued when it is called.
func TestDrainer_CloseDrainsQueuedResults(t *testing.T) {
	t.Parallel()

	drainer, reader := newTestDrainer(t, testenv.NewLogger(t))

	failing := &resolvedResult{err: errors.New("broker unavailable")}
	for range 100 {
		drainer.Enqueue(t.Context(), failing)
	}

	require.NoError(t, drainer.Close(t.Context()))
	require.Equal(t, int64(100), counterValue(t, reader, "pubsub.publish.ack_failures", testComponent))
}

// TestDrainer_CloseAbortsOnExpiredContext verifies shutdown stays bounded by
// the caller's deadline when a result never resolves, and that no worker
// outlives the Close.
func TestDrainer_CloseAbortsOnExpiredContext(t *testing.T) {
	t.Parallel()

	drainer, _ := newTestDrainer(t, testenv.NewLogger(t))

	stuck := &blockingResult{release: make(chan struct{})}
	drainer.Enqueue(t.Context(), stuck)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, drainer.Close(ctx), context.Canceled)

	// Close only returns once the workers have exited, so the barrier is
	// already satisfied and cannot hang here.
	drainer.Wait()
}

// TestDrainer_ThrottlesFailureLogs verifies a broker outage produces one log
// line rather than one per publish, and that the line reports how many it
// stood in for.
func TestDrainer_ThrottlesFailureLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelError,
		ReplaceAttr: nil,
	}))

	drainer, _ := newTestDrainer(t, logger)

	failing := &resolvedResult{err: errors.New("broker unavailable")}
	for range 25 {
		drainer.Enqueue(t.Context(), failing)
	}
	require.NoError(t, drainer.Close(t.Context()))

	require.Equal(t, 1, strings.Count(buf.String(), testLogMsg),
		"a correlated failure burst must not emit one log line per publish")
	require.Contains(t, buf.String(), string(attr.PubSubDrainSuppressedLogsKey))
}
