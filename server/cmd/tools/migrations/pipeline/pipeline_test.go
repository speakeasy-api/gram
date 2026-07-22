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

// erroringSource emits its items then fails, exercising the source-error edge:
// srcCh must not be closed on failure so the sink never sees a false EOF.
type erroringSource struct{ items []int }

func (s *erroringSource) Read(ctx context.Context, _ pipeline.Criteria, out chan<- int) error {
	for _, i := range s.items {
		select {
		case out <- i:
		case <-ctx.Done():
			return fmt.Errorf("source cancelled: %w", ctx.Err())
		}
	}
	return errBoom
}

// failOnSecond passes the first record through then errors on the second, so a
// prior record is buffered in the sink when the failure hits.
type failOnSecond struct{ seen int }

func (f *failOnSecond) Transform(_ context.Context, in int) ([]int, error) {
	f.seen++
	if f.seen >= 2 {
		return nil, errBoom
	}
	return []int{in}, nil
}

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

// fanout emits n copies of each input.
type fanout struct{ n int }

func (f fanout) Transform(_ context.Context, in int) ([]int, error) {
	out := make([]int, f.n)
	for i := range out {
		out[i] = in
	}
	return out, nil
}

// oddOnly drops even inputs (returns an empty slice) and passes odds through.
type oddOnly struct{}

func (oddOnly) Transform(_ context.Context, in int) ([]int, error) {
	if in%2 == 0 {
		return nil, nil
	}
	return []int{in}, nil
}

// cancelSink cancels the shared context the first time it receives a record.
type cancelSink struct {
	in     chan int
	cancel context.CancelFunc
	mu     sync.Mutex
	got    []int
}

func (s *cancelSink) Input() chan<- int { return s.in }

func (s *cancelSink) Run(ctx context.Context) error {
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
			s.cancel()
		}
	}
}

// batchSink flushes every batchSize records and once more on channel close,
// recording every flushed record and counting flushes.
type batchSink struct {
	in        chan int
	batchSize int
	mu        sync.Mutex
	got       []int
	flushes   int
}

func (s *batchSink) Input() chan<- int { return s.in }

func (s *batchSink) Run(ctx context.Context) error {
	buf := make([]int, 0, s.batchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		s.mu.Lock()
		s.got = append(s.got, buf...)
		s.flushes++
		s.mu.Unlock()
		buf = buf[:0]
	}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("sink cancelled: %w", ctx.Err())
		case v, ok := <-s.in:
			if !ok {
				flush()
				return nil
			}
			buf = append(buf, v)
			if len(buf) >= s.batchSize {
				flush()
			}
		}
	}
}

func TestRunFansOut(t *testing.T) {
	t.Parallel()

	src := &fakeSource{items: []int{1, 2, 3}}
	sink := newCollectSink(8)

	err := pipeline.Run[int, int](t.Context(), src, fanout{n: 3}, sink, pipeline.Criteria{}, 2)
	require.NoError(t, err)

	sort.Ints(sink.got)
	require.Equal(t, []int{1, 1, 1, 2, 2, 2, 3, 3, 3}, sink.got)
}

func TestRunDropsFilteredRecords(t *testing.T) {
	t.Parallel()

	src := &fakeSource{items: []int{1, 2, 3, 4, 5, 6}}
	sink := newCollectSink(8)

	err := pipeline.Run[int, int](t.Context(), src, oddOnly{}, sink, pipeline.Criteria{}, 2)
	require.NoError(t, err)

	sort.Ints(sink.got)
	require.Equal(t, []int{1, 3, 5}, sink.got)
}

func TestRunCancelsPromptly(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	// Many items with an unbuffered source channel keep the source/transform
	// stages actively producing when the sink cancels, so cancellation must
	// unwind them rather than drain the whole source.
	src := &fakeSource{items: make([]int, 1000)}
	sink := &cancelSink{in: make(chan int, 1), cancel: cancel}

	err := pipeline.Run[int, int](ctx, src, doubler{}, sink, pipeline.Criteria{}, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRunFlushesFinalPartialBatch(t *testing.T) {
	t.Parallel()

	src := &fakeSource{items: []int{1, 2, 3, 4, 5}}
	sink := &batchSink{in: make(chan int, 4), batchSize: 2}

	err := pipeline.Run[int, int](t.Context(), src, doubler{}, sink, pipeline.Criteria{}, 2)
	require.NoError(t, err)

	sort.Ints(sink.got)
	require.Equal(t, []int{2, 4, 6, 8, 10}, sink.got)
	// Two full batches (2+2) plus the partial final batch (1) flushed on close.
	require.Equal(t, 3, sink.flushes)
}

func TestRunDoesNotFlushOnUpstreamError(t *testing.T) {
	t.Parallel()

	// batchSize larger than the run so the buffered first record only ever
	// reaches the destination via a close-triggered flush — which must NOT happen
	// on a transform error.
	src := &fakeSource{items: []int{1, 2, 3}}
	sink := &batchSink{in: make(chan int, 4), batchSize: 10}

	err := pipeline.Run[int, int](t.Context(), src, &failOnSecond{}, sink, pipeline.Criteria{}, 2)
	require.Error(t, err)
	require.ErrorIs(t, err, errBoom)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	require.Empty(t, sink.got, "sink must not flush a partial batch when the pipeline fails")
	require.Zero(t, sink.flushes)
}

func TestRunDoesNotFlushOnSourceError(t *testing.T) {
	t.Parallel()

	// A source that emits rows then fails must not be mistaken for clean EOF:
	// the sink must not flush the buffered rows.
	src := &erroringSource{items: []int{1, 2, 3}}
	sink := &batchSink{in: make(chan int, 8), batchSize: 10}

	err := pipeline.Run[int, int](t.Context(), src, doubler{}, sink, pipeline.Criteria{}, 8)
	require.Error(t, err)
	require.ErrorIs(t, err, errBoom)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	require.Empty(t, sink.got, "sink must not flush when the source fails")
	require.Zero(t, sink.flushes)
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
