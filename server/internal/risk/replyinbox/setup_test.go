package replyinbox

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

type inboxTestEnv struct {
	redis  *miniredis.Miniredis
	client *redis.Client
	reader *sdkmetric.ManualReader
	inbox  *Inbox
	writer *Writer
}

func newTestLogger() *slog.Logger {
	return slog.Default() //nolint:forbidigo // testenv imports replyinbox through scanner fixtures.
}

func setupInboxTest(t *testing.T, replicaID string) *inboxTestEnv {
	t.Helper()
	return setupInboxTestWithDrainGate(t, replicaID, nil)
}

func setupInboxTestWithDrainGate(t *testing.T, replicaID string, drainGate <-chan struct{}) *inboxTestEnv {
	t.Helper()
	return setupInboxTestWithDrainerAndGate(t, replicaID, drainGate, nil)
}

func setupInboxTestWithDrainer(t *testing.T, replicaID string, drainFunc func(context.Context)) *inboxTestEnv {
	t.Helper()
	return setupInboxTestWithDrainerAndGate(t, replicaID, nil, drainFunc)
}

func setupInboxTestWithDrainerAndGate(t *testing.T, replicaID string, drainGate <-chan struct{}, drainFunc func(context.Context)) *inboxTestEnv {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), Protocol: 2})
	t.Cleanup(func() { _ = client.Close() })
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	inbox, err := New(t.Context(), newTestLogger(), otel.GetTracerProvider(), meterProvider, Config{
		RedisOptions: redis.Options{Addr: mr.Addr(), Protocol: 2},
		ReplicaID:    replicaID,
		BlockTimeout: time.Second,
		DrainGate:    drainGate,
		drainFunc:    drainFunc,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = inbox.Close() })
	return &inboxTestEnv{redis: mr, client: client, reader: reader, inbox: inbox, writer: NewWriter(client)}
}

func waitForWaiter(t *testing.T, inbox *Inbox, scanID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, ok := inbox.waiters.Load(scanID)
		return ok
	}, time.Second, 5*time.Millisecond)
}
