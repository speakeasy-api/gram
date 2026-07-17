package aiintegrations

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
	timeWindowPageBufferSize  = 1
	timeWindowHeartbeatPeriod = 10 * time.Second
)

type timeWindowPage struct {
	Rows     []telemetry.LogParams
	NextPage string
	HasMore  bool
}

// timeWindowSource adapts one provider report to the shared time-window
// poller. Implementations make one API call and map one response page. The
// poller owns pagination, rate-limit waits, heartbeats, writes, and durable
// watermark advancement.
type timeWindowSource interface {
	// UpperBound returns the exclusive upper bound for pulls ending at
	// endTime, e.g. a provider finalization watermark like Anthropic's
	// data_refreshed_at. Sources whose data is immediately final return
	// endTime.
	UpperBound(ctx context.Context, endTime time.Time) (time.Time, error)
	// FetchPage fetches one response page for the fixed window. page is empty
	// for the first request and otherwise the opaque value returned by the
	// preceding response.
	FetchPage(ctx context.Context, start, end time.Time, page string) (timeWindowPage, error)
	// RetryAfter translates a provider rate-limit error into a requested wait.
	// A zero duration asks the poller to apply its default delay.
	RetryAfter(err error) (time.Duration, bool)
}

type timeWindowCheckpointStore interface {
	AdvanceSchedulePollWatermark(ctx context.Context, configID uuid.UUID, schedule string, watermark time.Time) error
}

type telemetryBulkLogger interface {
	LogBulkDeduplicated(ctx context.Context, params []telemetry.LogParams) error
}

// timeWindowPoller drives a time-kind sync schedule: it walks the range from
// the schedule's watermark to the source's upper bound one window at a time,
// pipelines page fetches with page-sized bulk writes, and advances the
// watermark only after every page in a window is durable.
type timeWindowPoller struct {
	store           timeWindowCheckpointStore
	telemetryLogger telemetryBulkLogger
	schedule        string
	heartbeat       func(ctx context.Context, page int)
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

func (p *timeWindowPoller) sync(ctx context.Context, cfg Config, watermarkAt time.Time, source timeWindowSource, endTime time.Time) error {
	p.heartbeat(ctx, 0)
	upperBound, err := p.upperBound(ctx, source, endTime)
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

		if err := p.fetchAndWriteWindow(ctx, source, windowStart, windowEnd); err != nil {
			return err
		}

		if err := p.store.AdvanceSchedulePollWatermark(ctx, cfg.ID, p.schedule, windowEnd); err != nil {
			return fmt.Errorf("advance %s watermark: %w", p.schedule, err)
		}
		windowStart = windowEnd
	}
	return nil
}

func (p *timeWindowPoller) upperBound(ctx context.Context, source timeWindowSource, endTime time.Time) (time.Time, error) {
	for {
		upperBound, err := source.UpperBound(ctx, endTime)
		if err == nil {
			return upperBound, nil
		}

		retryAfter, retry := source.RetryAfter(err)
		if !retry {
			return time.Time{}, fmt.Errorf("get provider upper bound: %w", err)
		}
		if err := p.waitForRetry(ctx, retryAfter, 0); err != nil {
			return time.Time{}, err
		}
		p.heartbeat(ctx, 0)
	}
}

// fetchAndWriteWindow overlaps fetching page N+1 with writing page N. A
// provider fetch failure is delivered through fetchDone instead of being
// returned directly from the producer goroutine so the writer can finish any
// already-fetched pages before the error cancels the errgroup context.
func (p *timeWindowPoller) fetchAndWriteWindow(ctx context.Context, source timeWindowSource, start, end time.Time) error {
	group, groupCtx := errgroup.WithContext(ctx)
	pages := make(chan timeWindowPage, timeWindowPageBufferSize)
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

			var page timeWindowPage
			for {
				page, fetchErr = source.FetchPage(groupCtx, start, end, pageToken)
				if fetchErr == nil {
					break
				}

				retryAfter, retry := source.RetryAfter(fetchErr)
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
			case pages <- page:
			}

			if !page.HasMore {
				return nil
			}
			if page.NextPage == "" {
				fetchErr = fmt.Errorf("fetch %s page %d: provider returned has_more without a next page", p.schedule, pageNum)
				return nil
			}
			pageToken = page.NextPage
		}
	})

	group.Go(func() error {
		for page := range pages {
			if len(page.Rows) == 0 {
				continue
			}
			if err := p.telemetryLogger.LogBulkDeduplicated(groupCtx, page.Rows); err != nil {
				return oops.E(oops.CodeUnexpected, err, "insert ai integration telemetry page")
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

func (p *timeWindowPoller) waitForRetry(ctx context.Context, retryAfter time.Duration, page int) error {
	if retryAfter <= 0 {
		retryAfter = time.Minute
	}
	retryAfter += time.Duration(time.Now().UnixNano() % int64(time.Second))

	deadline := time.Now().Add(retryAfter)
	for remaining := time.Until(deadline); remaining > 0; remaining = time.Until(deadline) {
		p.heartbeat(ctx, page)
		timer := time.NewTimer(min(remaining, timeWindowHeartbeatPeriod))
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for retry: %w", context.Cause(ctx))
		case <-timer.C:
		}
	}
	return nil
}
