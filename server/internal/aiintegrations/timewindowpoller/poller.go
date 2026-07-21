package timewindowpoller

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	pageBufferSize  = 1
	heartbeatPeriod = 10 * time.Second
)

type Page[T any] struct {
	Payload  T
	NextPage string
	HasMore  bool
}

// Source adapts one provider report to the shared time-window poller.
// Implementations make one API call and map one response page. The poller owns
// pagination, rate-limit waits, heartbeats, and durable watermark advancement.
type Source[T any] interface {
	// UpperBound returns the exclusive upper bound for pulls ending at
	// endTime, e.g. a provider finalization watermark like Anthropic's
	// data_refreshed_at. Sources whose data is immediately final return
	// endTime.
	UpperBound(ctx context.Context, endTime time.Time) (time.Time, error)
	// FetchPage fetches one response page for the fixed window. pageToken is
	// empty for the first request and otherwise the opaque value returned by
	// the preceding response.
	FetchPage(ctx context.Context, start, end time.Time, pageToken string) (Page[T], error)
	// ProcessPage durably handles one fetched page before the watermark can
	// advance.
	ProcessPage(ctx context.Context, payload T) error
	// RetryAfter translates a provider rate-limit error into a requested wait.
	// A zero duration asks the poller to apply its default delay.
	RetryAfter(err error) (time.Duration, bool)
}

type Store interface {
	AdvanceWatermark(ctx context.Context, syncID uuid.UUID, checkpoint PollCheckpoint) error
}

type SyncState struct {
	SyncID      uuid.UUID
	WatermarkAt time.Time
	Checkpoint  PollCheckpoint
}

// Poller drives a time-kind sync schedule: it walks the range from the
// schedule's watermark to the source's upper bound one window at a time,
// pipelines page fetches with page processing, and advances the watermark only
// after every page in a window is durable.
type Poller[T any] struct {
	Store     Store
	Schedule  string
	State     SyncState
	Source    Source[T]
	EndTime   time.Time
	Heartbeat func(ctx context.Context, page int)
	// InitialLookback bounds the first window for a schedule that has never
	// synced (zero watermark).
	InitialLookback time.Duration
	// MaxWindow caps a single fetch (e.g. the Admin Analytics 24h limit for
	// 1m buckets); zero means one window covers the whole range.
	MaxWindow time.Duration
	// Granularity truncates window bounds, e.g. to whole minutes for bucketed
	// reports; zero leaves bounds untouched.
	Granularity time.Duration
	// ResumeOffset shifts a fetch's start past its window start when that
	// start is a completed watermark — the inclusive end of already-ingested
	// data — so the boundary event is not fetched twice. Sources with
	// inclusive-end windows (Cursor, millisecond timestamps) set the smallest
	// representable step; exclusive-end bucketed sources (Admin Analytics)
	// leave it zero. The initial-lookback boundary was never fetched, so it
	// is exempt from the offset.
	ResumeOffset time.Duration
}

func (p *Poller[T]) Do(ctx context.Context) error {
	p.Heartbeat(ctx, 0)
	upperBound, err := p.upperBound(ctx)
	if err != nil {
		return fmt.Errorf("fetch %s upper bound: %w", p.Schedule, err)
	}

	checkpoint := p.State.Checkpoint
	if checkpoint.V == 0 {
		checkpoint = CompletedCheckpoint(p.State.WatermarkAt)
	}

	windowStart := checkpoint.Watermark.UTC()
	// A non-zero watermark is the inclusive end of already-ingested data and
	// must be skipped by ResumeOffset; the synthetic initial-lookback boundary
	// was never fetched and must not be, or the event exactly on it would be
	// permanently missed.
	resumed := !windowStart.IsZero()
	if !resumed {
		windowStart = p.EndTime.Add(-p.InitialLookback)
	}
	windowStart = windowStart.Truncate(p.Granularity)
	desiredEnd := minTime(p.EndTime.UTC(), upperBound.UTC()).Truncate(p.Granularity)

	for windowStart.Before(desiredEnd) {
		windowEnd := desiredEnd
		if p.MaxWindow > 0 && windowStart.Add(p.MaxWindow).Before(desiredEnd) {
			windowEnd = windowStart.Add(p.MaxWindow)
		}

		fetchStart := windowStart
		pageToken := ""
		checkpointWatermark := windowStart
		if checkpoint.Partial() {
			checkpointWatermark = checkpoint.Watermark
			fetchStart = checkpoint.WindowStart
			windowEnd = checkpoint.WindowEnd
			pageToken = checkpoint.PageToken
		} else if resumed {
			fetchStart = fetchStart.Add(p.ResumeOffset)
		}

		group, groupCtx := errgroup.WithContext(ctx)
		pages := make(chan Page[T], pageBufferSize)
		fetchDone := make(chan error, 1)

		group.Go(func() error {
			p.fetchWindowPages(groupCtx, fetchStart, windowEnd, pageToken, pages, fetchDone)
			return nil
		})

		group.Go(func() error {
			for current := range pages {
				if err := p.Source.ProcessPage(groupCtx, current.Payload); err != nil {
					return fmt.Errorf("process %s page: %w", p.Schedule, err)
				}

				next := CompletedCheckpoint(windowEnd)
				if current.HasMore {
					next = PartialCheckpoint(checkpointWatermark, fetchStart, windowEnd, current.NextPage)
				}
				if err := p.Store.AdvanceWatermark(ctx, p.State.SyncID, next); err != nil {
					return fmt.Errorf("advance %s watermark: %w", p.Schedule, err)
				}
			}
			if err := <-fetchDone; err != nil {
				return err
			}
			return nil
		})

		if err := group.Wait(); err != nil {
			return fmt.Errorf("process %s window: %w", p.Schedule, err)
		}
		windowStart = windowEnd
		checkpoint = CompletedCheckpoint(windowStart)
		// Later windows start at the previous window's fetched end, which is
		// a completed boundary like a stored watermark.
		resumed = true
	}
	return nil
}

func (p *Poller[T]) upperBound(ctx context.Context) (time.Time, error) {
	for {
		upperBound, err := p.Source.UpperBound(ctx, p.EndTime)
		if err == nil {
			return upperBound, nil
		}

		retryAfter, retry := p.Source.RetryAfter(err)
		if !retry {
			return time.Time{}, fmt.Errorf("get provider upper bound: %w", err)
		}
		if err := p.waitForRetry(ctx, retryAfter, 0); err != nil {
			return time.Time{}, err
		}
		p.Heartbeat(ctx, 0)
	}
}

// fetchWindowPages overlaps fetching page N+1 with processing page N. A
// provider fetch failure is delivered through fetchDone instead of being
// returned directly from the producer goroutine so the consumer can finish any
// already-fetched pages before the error cancels the errgroup context.
func (p *Poller[T]) fetchWindowPages(ctx context.Context, start, end time.Time, pageToken string, pages chan<- Page[T], fetchDone chan<- error) {
	var fetchErr error
	defer close(pages)
	defer func() {
		fetchDone <- fetchErr
		close(fetchDone)
	}()

	for pageNum := 1; ; pageNum++ {
		p.Heartbeat(ctx, pageNum)

		var current Page[T]
		for {
			current, fetchErr = p.Source.FetchPage(ctx, start, end, pageToken)
			if fetchErr == nil {
				break
			}

			retryAfter, retry := p.Source.RetryAfter(fetchErr)
			if !retry {
				fetchErr = fmt.Errorf("fetch %s page %d: %w", p.Schedule, pageNum, fetchErr)
				return
			}
			if fetchErr = p.waitForRetry(ctx, retryAfter, pageNum); fetchErr != nil {
				fetchErr = fmt.Errorf("wait to retry %s page %d: %w", p.Schedule, pageNum, fetchErr)
				return
			}
			p.Heartbeat(ctx, pageNum)
		}

		if current.HasMore && current.NextPage == "" {
			fetchErr = fmt.Errorf("fetch %s page %d: provider returned has_more without a next page", p.Schedule, pageNum)
			return
		}

		select {
		case <-ctx.Done():
			fetchErr = ctx.Err()
			return
		case pages <- current:
		}

		if !current.HasMore {
			return
		}
		pageToken = current.NextPage
	}
}

func (p *Poller[T]) waitForRetry(ctx context.Context, retryAfter time.Duration, page int) error {
	if retryAfter <= 0 {
		retryAfter = time.Minute
	}
	retryAfter += time.Duration(time.Now().UnixNano() % int64(time.Second))

	deadline := time.Now().Add(retryAfter)
	for remaining := time.Until(deadline); remaining > 0; remaining = time.Until(deadline) {
		p.Heartbeat(ctx, page)
		timer := time.NewTimer(min(remaining, heartbeatPeriod))
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for retry: %w", context.Cause(ctx))
		case <-timer.C:
		}
	}
	return nil
}

// minTime returns the earlier of two times, treating the zero value as "unset"
// rather than earliest.
func minTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() {
		return a
	}
	if a.Before(b) {
		return a
	}
	return b
}
