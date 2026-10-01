package productmetrics

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/infra/gen"
	pmv1 "github.com/speakeasy-api/gram/infra/gen/gram/productmetrics/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type retryInserter struct {
	repo     *Repository
	attempts atomic.Int32
	inserted chan struct{}
}

func (i *retryInserter) Insert(ctx context.Context, rows []Contribution) error {
	if i.attempts.Add(1) == 1 {
		return context.DeadlineExceeded
	}
	if err := i.repo.Insert(ctx, rows); err != nil {
		return err
	}
	select {
	case i.inserted <- struct{}{}:
	default:
	}
	return nil
}

func TestPubSubToRollup(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, os.Getenv("PUBSUB_EMULATOR_HOST"), "requires the local Pub/Sub emulator")
	conn := newTestClickhouse(t)
	now := time.Now().UTC()
	repo := NewRepository(conn, func() time.Time { return now })
	c := synthetic(Counter)
	c.EventTime = now.Truncate(time.Minute).Add(-time.Minute)
	c.ObservedAt = now
	h := c
	h.Definition.Name = "gram.synthetic.histogram"
	h.Definition.Instrument = Histogram
	h.ID = "histogram-observation"
	h.Value = Float(2.5)
	r, err := NewRegistry(c.Definition, h.Definition)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	project := "synthetic-metrics-" + uuid.NewString()
	client, err := pubsub.NewClient(ctx, project)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	broker := gcp.NewEmulatedPubSub(testenv.NewLogger(t), project, client, gen.Descriptors)
	sub, err := gcp.PubSubSubscriberForMessage(ctx, broker, &pmv1.Contribution{}, &pmv1.Processor{}, gcp.WithDiscardMalformedMessages())
	require.NoError(t, err)
	pub, err := gcp.PubSubPublisherForMessage(ctx, broker, &pmv1.Contribution{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pub.Stop(context.Background())) })
	i := &retryInserter{repo: repo, attempts: atomic.Int32{}, inserted: make(chan struct{}, 2)}
	w, err := NewWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), r, i, func() time.Time { return now })
	require.NoError(t, err)
	var invalidDeliveries atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- sub.ReceiveBatchWithResult(ctx, gcp.BatchReceiveSettings{MaxMessages: 3, MaxBytes: BatchMaxBytes, MaxLatency: 100 * time.Millisecond}, func(ctx context.Context, batch []gcp.BatchMessage[*pmv1.Contribution]) error {
			for _, m := range batch {
				if m.Message.GetContributionId() == "invalid" {
					invalidDeliveries.Add(1)
				}
			}
			return w.HandleBatchWithResult(ctx, batch)
		})
	}()
	defer func() {
		cancel()
		err := <-done
		require.True(t, err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded), "%v", err)
	}()
	p := NewPublisher(r, pub)
	_, err = p.Publish(ctx, c).Get(ctx)
	require.NoError(t, err)
	_, err = p.Publish(ctx, h).Get(ctx)
	require.NoError(t, err)
	bad := pmv1.Contribution_builder{ContributionId: new("invalid")}.Build()
	_, err = pub.Publish(ctx, bad).Get(ctx)
	require.NoError(t, err)
	// Query only after insertion acknowledgements, never after a sleep.
	for _, input := range []Contribution{c, h} {
		q := Query{Tenant: input.Tenant, Definition: input.Definition, Start: c.EventTime, End: c.EventTime.Add(time.Minute), Interval: time.Minute}
		for {
			results, err := repo.Query(ctx, q)
			require.NoError(t, err)
			if len(results) > 0 {
				require.Equal(t, uint64(1), results[0].Count)
				break
			}
			select {
			case <-i.inserted:
			case <-ctx.Done():
				require.FailNow(t, "timed out waiting for rollup")
			}
		}
	}
	require.GreaterOrEqual(t, i.attempts.Load(), int32(2))
	require.Equal(t, int32(1), invalidDeliveries.Load(), "invalid messages must be acknowledged even beside transient failures")
}
