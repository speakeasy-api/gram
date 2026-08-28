package metering

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

type blockingStripeCustomerReader struct {
	calls       atomic.Int32
	callStarted chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func (r *blockingStripeCustomerReader) GetStripeCustomerID(ctx context.Context, _ string) (pgtype.Text, error) {
	r.calls.Add(1)
	r.callStarted <- struct{}{}
	select {
	case <-r.release:
		return pgtype.Text{String: "cus_coalesced", Valid: true}, nil
	case <-ctx.Done():
		return pgtype.Text{}, fmt.Errorf("wait for lookup release: %w", ctx.Err())
	}
}

func (r *blockingStripeCustomerReader) Release() {
	r.releaseOnce.Do(func() { close(r.release) })
}

type countingV2MeterEventClient struct {
	calls atomic.Int32
}

func (c *countingV2MeterEventClient) CreateMeterEvent(context.Context, stripeclient.V2MeterEventInput) error {
	c.calls.Add(1)
	return nil
}

func TestMeterReadingStripeExporterCoalescesConcurrentCustomerLookups(t *testing.T) {
	t.Parallel()

	const deliveries = 32

	lookup := &blockingStripeCustomerReader{
		calls:       atomic.Int32{},
		callStarted: make(chan struct{}, deliveries),
		release:     make(chan struct{}),
		releaseOnce: sync.Once{},
	}
	defer lookup.Release()
	client := &countingV2MeterEventClient{calls: atomic.Int32{}}
	exporter := NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), nil, client)
	exporter.stripeCustomers = lookup

	start := make(chan struct{})
	ready := make(chan struct{}, deliveries)
	results := make(chan error, deliveries)
	for i := range deliveries {
		go func() {
			ready <- struct{}{}
			<-start
			reading := new(meteringv1.MeterReading)
			reading.SetId(fmt.Sprintf("reading-%d", i))
			reading.SetOrganizationId("organization")
			reading.SetKind(meteringv1.MeterReading_KIND_USAGE)
			reading.SetMeterId(string(MeterAgentSessionStorage))
			reading.SetMeterVersion(1)
			reading.SetValue(1)
			reading.SetOccurredAt(time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano))
			results <- exporter.Handle(t.Context(), reading, gcp.MessageMetadata{})
		}()
	}
	for range deliveries {
		<-ready
	}
	close(start)

	<-lookup.callStarted
	select {
	case <-lookup.callStarted:
		require.FailNow(t, "concurrent cache misses performed duplicate customer lookups")
	case <-time.After(250 * time.Millisecond):
	}
	lookup.Release()

	for range deliveries {
		require.NoError(t, <-results)
	}
	require.Equal(t, int32(1), lookup.calls.Load())
	require.Equal(t, int32(deliveries), client.calls.Load())
}
