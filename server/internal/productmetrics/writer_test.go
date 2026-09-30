package productmetrics

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	pmv1 "github.com/speakeasy-api/gram/infra/gen/gram/productmetrics/v1"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

type captureInserter struct {
	rows []Contribution
	err  error
}

func (i *captureInserter) Insert(_ context.Context, rows []Contribution) error {
	i.rows = append(i.rows, rows...)
	return i.err
}

func TestWriterMixedBatchRetry(t *testing.T) {
	t.Parallel()
	c := synthetic(Counter)
	now := c.ObservedAt.Add(time.Minute)
	r, err := NewRegistry(c.Definition)
	require.NoError(t, err)
	inserter := &captureInserter{err: context.DeadlineExceeded}
	w, err := NewWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), r, inserter, func() time.Time { return now })
	require.NoError(t, err)
	m, err := Encode(c)
	require.NoError(t, err)
	distinct := c
	distinct.ID = "observation-2"
	m2, err := Encode(distinct)
	require.NoError(t, err)
	conflict := c
	conflict.Value = Integer(9)
	mc, err := Encode(conflict)
	require.NoError(t, err)
	indexes, err := w.process(t.Context(), []*pmv1.Contribution{nil, m, m, m2, mc})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, []int{1, 2, 3}, indexes)
	require.Len(t, inserter.rows, 2)
	// Ambiguous timeout may follow persistence. Replaying a new batch includes
	// the observations again: there is deliberately no cross-batch suppression.
	inserter.err = nil
	indexes, err = w.process(t.Context(), []*pmv1.Contribution{m, m2})
	require.NoError(t, err)
	require.Nil(t, indexes)
	require.Len(t, inserter.rows, 4)
}

func TestWriterEnforcesCodeOwnedDimensions(t *testing.T) {
	t.Parallel()
	c := synthetic(Counter)
	c.PointAttributes = []attribute.KeyValue{attribute.String("request_id", "unique")}
	r, err := NewRegistry(c.Definition)
	require.NoError(t, err)
	require.NoError(t, r.RegisterDimensions(c.Definition))
	inserter := &captureInserter{}
	w, err := NewWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), r, inserter, func() time.Time { return c.ObservedAt })
	require.NoError(t, err)
	m, err := Encode(c)
	require.NoError(t, err)
	_, err = w.process(t.Context(), []*pmv1.Contribution{m})
	require.NoError(t, err)
	require.Empty(t, inserter.rows)
	c.ResourceAttributes = nil
	c.ScopeAttributes = nil
	c.PointAttributes = nil
	m, err = Encode(c)
	require.NoError(t, err)
	_, err = w.process(t.Context(), []*pmv1.Contribution{m})
	require.NoError(t, err)
	require.Len(t, inserter.rows, 1)
}

func TestWriterRetentionAndRegistry(t *testing.T) {
	t.Parallel()
	c := synthetic(Counter)
	now := c.ObservedAt
	r, err := NewRegistry(c.Definition)
	require.NoError(t, err)
	inserter := &captureInserter{err: errors.New("should not insert")}
	w, err := NewWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), r, inserter, func() time.Time { return now })
	require.NoError(t, err)
	cases := []struct {
		name   string
		change func(*Contribution)
	}{
		{"expired", func(c *Contribution) { c.EventTime = EarliestBucket(now).Add(-time.Nanosecond) }},
		{"future", func(c *Contribution) { c.EventTime = now.Add(MaxFutureSkew + time.Nanosecond) }},
		{"unregistered", func(c *Contribution) { c.Definition.Name = "gram.synthetic.unregistered" }},
		{"descriptor conflict", func(c *Contribution) { c.Definition.Unit = "s" }},
	}
	// One shared writer tests that rejected inputs never reach the inserter.
	for _, tc := range cases {
		input := c
		tc.change(&input)
		m, err := Encode(input)
		require.NoError(t, err, tc.name)
		indexes, err := w.process(t.Context(), []*pmv1.Contribution{m})
		require.NoError(t, err, tc.name)
		require.Nil(t, indexes)
	}
	require.Empty(t, inserter.rows)
}

type blockedInserter struct {
	entered chan struct{}
	release chan struct{}
}

func (i *blockedInserter) Insert(ctx context.Context, _ []Contribution) error {
	close(i.entered)
	select {
	case <-i.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for insertion: %w", ctx.Err())
	}
}

func TestWriterWaitsForDurability(t *testing.T) {
	t.Parallel()
	c := synthetic(Counter)
	r, err := NewRegistry(c.Definition)
	require.NoError(t, err)
	i := &blockedInserter{entered: make(chan struct{}), release: make(chan struct{})}
	w, err := NewWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), r, i, func() time.Time { return c.ObservedAt })
	require.NoError(t, err)
	m, err := Encode(c)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := w.process(t.Context(), []*pmv1.Contribution{m}); done <- err }()
	<-i.entered
	select {
	case err := <-done:
		require.FailNow(t, "handler settled before durable insertion", "%v", err)
	default:
	}
	close(i.release)
	require.NoError(t, <-done)
}
