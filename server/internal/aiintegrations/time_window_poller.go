package aiintegrations

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

type page[T any] struct {
	Payload  T
	NextPage string
	HasMore  bool
}

// source adapts one provider report to the shared time-window
// poller. Implementations make one API call and map one response page. The
// poller owns pagination, rate-limit waits, heartbeats, processing, and durable
// watermark advancement.
type source[T any] interface {
	// UpperBound returns the exclusive upper bound for pulls ending at
	// endTime, e.g. a provider finalization watermark like Anthropic's
	// data_refreshed_at. Sources whose data is immediately final return
	// endTime.
	UpperBound(ctx context.Context, endTime time.Time) (time.Time, error)
	// FetchPage fetches one response page for the fixed window. pageToken is
	// empty for the first request and otherwise the opaque value returned by
	// the preceding response.
	FetchPage(ctx context.Context, start, end time.Time, pageToken string) (page[T], error)
	// RetryAfter translates a provider rate-limit error into a requested wait.
	// A zero duration asks the poller to apply its default delay.
	RetryAfter(err error) (time.Duration, bool)
}

type checkpointStore interface {
	AdvanceSchedulePollWatermark(ctx context.Context, configID uuid.UUID, schedule string, watermark time.Time) error
}

// poller drives a time-kind sync schedule: it walks the range from
// the schedule's watermark to the source's upper bound one window at a time,
// pipelines page fetches with page processing, and advances the
// watermark only after every page in a window is durable.
type poller[T any] struct {
	store       checkpointStore
	schedule    string
	heartbeat   func(ctx context.Context, page int)
	processPage func(ctx context.Context, payload T) error
	// initialLookback bounds the first window for a schedule that has never
	// synced (zero watermark).
	initialLookback time.Duration
	// maxWindow caps a single fetch (e.g. the Admin Analytics 24h limit for
	// 1m buckets); zero means one window covers the whole range.
	maxWindow time.Duration
	// granularity truncates window bounds, e.g. to whole minutes for
	// bucketed reports; zero leaves bounds untouched.
	granularity time.Duration
}

func (p *poller[T]) sync(ctx context.Context, cfg Config, watermarkAt time.Time, src source[T], endTime time.Time) error {
	p.heartbeat(ctx, 0)
	upperBound, err := p.upperBound(ctx, src, endTime)
	if err != nil {
		return fmt.Errorf("fetch %s upper bound: %w", p.schedule, err)
	}

	windowStart := watermarkAt.UTC()
	if windowStart.IsZero() {
		windowStart = endTime.Add(-p.initialLookback)
	}
	windowStart = windowStart.Truncate(p.granularity)
	desiredEnd := minTime(endTime.UTC(), upperBound.UTC()).Truncate(p.granularity)

	for windowStart.Before(desiredEnd) {
		windowEnd := desiredEnd
		if p.maxWindow > 0 && windowStart.Add(p.maxWindow).Before(desiredEnd) {
			windowEnd = windowStart.Add(p.maxWindow)
		}

		if err := p.fetchAndProcessWindow(ctx, src, windowStart, windowEnd); err != nil {
			return err
		}

		if err := p.store.AdvanceSchedulePollWatermark(ctx, cfg.ID, p.schedule, windowEnd); err != nil {
			return fmt.Errorf("advance %s watermark: %w", p.schedule, err)
		}
		windowStart = windowEnd
	}
	return nil
}

func (p *poller[T]) upperBound(ctx context.Context, src source[T], endTime time.Time) (time.Time, error) {
	for {
		upperBound, err := src.UpperBound(ctx, endTime)
		if err == nil {
			return upperBound, nil
		}

		retryAfter, retry := src.RetryAfter(err)
		if !retry {
			return time.Time{}, fmt.Errorf("get provider upper bound: %w", err)
		}
		if err := p.waitForRetry(ctx, retryAfter, 0); err != nil {
			return time.Time{}, err
		}
		p.heartbeat(ctx, 0)
	}
}

// fetchAndProcessWindow overlaps fetching page N+1 with processing page N. A
// provider fetch failure is delivered through fetchDone instead of being
// returned directly from the producer goroutine so the consumer can finish any
// already-fetched pages before the error cancels the errgroup context.
func (p *poller[T]) fetchAndProcessWindow(ctx context.Context, src source[T], start, end time.Time) error {
	group, groupCtx := errgroup.WithContext(ctx)
	pages := make(chan page[T], pageBufferSize)
	fetchDone := make(chan error, 1)

	group.Go(func() error {
		var fetchErr error
		defer close(pages)
		defer func() {
			fetchDone <- fetchErr
			close(fetchDone)
		}()

		pageToken := ""
		for pageNum := 1; ; pageNum++ {
			p.heartbeat(groupCtx, pageNum)

			var current page[T]
			for {
				current, fetchErr = src.FetchPage(groupCtx, start, end, pageToken)
				if fetchErr == nil {
					break
				}

				retryAfter, retry := src.RetryAfter(fetchErr)
				if !retry {
					fetchErr = fmt.Errorf("fetch %s page %d: %w", p.schedule, pageNum, fetchErr)
					return nil
				}
				if fetchErr = p.waitForRetry(groupCtx, retryAfter, pageNum); fetchErr != nil {
					fetchErr = fmt.Errorf("wait to retry %s page %d: %w", p.schedule, pageNum, fetchErr)
					return nil
				}
				p.heartbeat(groupCtx, pageNum)
			}

			select {
			case <-groupCtx.Done():
				fetchErr = groupCtx.Err()
				return nil
			case pages <- current:
			}

			if !current.HasMore {
				return nil
			}
			if current.NextPage == "" {
				fetchErr = fmt.Errorf("fetch %s page %d: provider returned has_more without a next page", p.schedule, pageNum)
				return nil
			}
			pageToken = current.NextPage
		}
	})

	group.Go(func() error {
		for current := range pages {
			if err := p.processPage(groupCtx, current.Payload); err != nil {
				return fmt.Errorf("process %s page: %w", p.schedule, err)
			}
		}
		if err := <-fetchDone; err != nil {
			return err
		}
		return nil
	})

	if err := group.Wait(); err != nil {
		return fmt.Errorf("process %s window: %w", p.schedule, err)
	}
	return nil
}

func (p *poller[T]) waitForRetry(ctx context.Context, retryAfter time.Duration, page int) error {
	if retryAfter <= 0 {
		retryAfter = time.Minute
	}
	retryAfter += time.Duration(time.Now().UnixNano() % int64(time.Second))

	deadline := time.Now().Add(retryAfter)
	for remaining := time.Until(deadline); remaining > 0; remaining = time.Until(deadline) {
		p.heartbeat(ctx, page)
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

// minTime returns the earlier of two times, treating the zero value as
// "unset" rather than earliest.
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
