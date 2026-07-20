package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
)

// fakeSource publishes a fixed slice of ints.
type fakeSource struct{ items []int }

func (f *fakeSource) Read(ctx context.Context, _ pipeline.Criteria, out chan<- int) error {
	for _, i := range f.items {
		select {
		case out <- i:
		case <-ctx.Done():
			return fmt.Errorf("source cancelled: %w", ctx.Err())
		}
	}
	return nil
}

// doubler maps n to 2n.
type doubler struct{}

func (doubler) Transform(_ context.Context, in int) ([]int, error) { return []int{in * 2}, nil }

// failing returns an error on the first item.
type failing struct{}

func (failing) Transform(_ context.Context, _ int) ([]int, error) { return nil, errBoom }

var errBoom = errors.New("boom")

// collectSink records everything it drains from its input channel.
type collectSink struct {
	in  chan int
	mu  sync.Mutex
	got []int
}

func newCollectSink(buf int) *collectSink { return &collectSink{in: make(chan int, buf)} }

func (s *collectSink) Input() chan<- int { return s.in }

func (s *collectSink) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("sink cancelled: %w", ctx.Err())
		case v, ok := <-s.in:
			if !ok {
				return nil
			}
			s.mu.Lock()
			s.got = append(s.got, v)
			s.mu.Unlock()
		}
	}
}

func TestRunProcessesEveryItem(t *testing.T) {
	t.Parallel()

	src := &fakeSource{items: []int{1, 2, 3, 4, 5}}
	sink := newCollectSink(4)

	err := pipeline.Run[int, int](t.Context(), src, doubler{}, sink, pipeline.Criteria{}, 2)
	require.NoError(t, err)

	sort.Ints(sink.got)
	require.Equal(t, []int{2, 4, 6, 8, 10}, sink.got)
}

func TestRunHandlesEmptySource(t *testing.T) {
	t.Parallel()

	src := &fakeSource{items: nil}
	sink := newCollectSink(1)

	err := pipeline.Run[int, int](t.Context(), src, doubler{}, sink, pipeline.Criteria{}, 1)
	require.NoError(t, err)
	require.Empty(t, sink.got)
}

func TestRunPropagatesTransformError(t *testing.T) {
	t.Parallel()

	src := &fakeSource{items: []int{1, 2, 3}}
	sink := newCollectSink(4)

	err := pipeline.Run[int, int](t.Context(), src, failing{}, sink, pipeline.Criteria{}, 2)
	require.Error(t, err)
	require.ErrorIs(t, err, errBoom)
}
