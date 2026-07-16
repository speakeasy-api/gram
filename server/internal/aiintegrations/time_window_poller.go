package aiintegrations

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// timeWindowSource fetches one provider report for a time-kind sync schedule.
// Implementations own the provider API client, pagination, rate limiting, and
// the mapping from provider rows to telemetry log rows.
type timeWindowSource interface {
	// UpperBound returns the exclusive upper bound for pulls ending at
	// endTime, e.g. a provider finalization watermark like Anthropic's
	// data_refreshed_at. Sources whose data is immediately final return
	// endTime.
	UpperBound(ctx context.Context, endTime time.Time) (time.Time, error)
	// FetchWindow returns the telemetry rows for the [start, end) window.
	FetchWindow(ctx context.Context, start, end time.Time) ([]telemetry.LogParams, error)
}

// timeWindowPoller drives a time-kind sync schedule: it walks the range from
// the schedule's watermark to the source's upper bound one window at a time,
// bulk-writes each window's rows, and advances the watermark after each
// durable write so a mid-sync crash re-fetches at most one window.
type timeWindowPoller struct {
	store           *Store
	telemetryLogger *telemetry.Logger
	schedule        string
	// pollInterval is the delay between polls, applied by maybeSync when it
	// records an outcome.
	pollInterval time.Duration
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
	upperBound, err := source.UpperBound(ctx, endTime)
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

		rows, err := source.FetchWindow(ctx, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("fetch %s window: %w", p.schedule, err)
		}
		if len(rows) > 0 {
			if err := p.telemetryLogger.LogBulk(ctx, rows); err != nil {
				return oops.E(oops.CodeUnexpected, err, "insert ai integration telemetry logs")
			}
		}

		if err := p.store.AdvanceSchedulePollWatermark(ctx, cfg.ID, p.schedule, windowEnd); err != nil {
			return fmt.Errorf("advance %s watermark: %w", p.schedule, err)
		}
		windowStart = windowEnd
	}
	return nil
}

// maybeSync runs the schedule when due and records the outcome on its sync
// row. Failures are recorded and logged but never returned: secondary
// schedules must not block the primary sync sharing the poll activity.
// classifyErr, when set, rewrites errors before they are logged and recorded
// (e.g. to explain provider access requirements).
func (p *timeWindowPoller) maybeSync(ctx context.Context, logger *slog.Logger, cfg Config, source timeWindowSource, endTime time.Time, classifyErr func(error) error) {
	logger = logger.With(attr.SlogAIIntegrationSyncSchedule(p.schedule))

	state, err := p.store.EnsureTimeSyncSchedule(ctx, cfg.ID, p.schedule)
	if err != nil {
		logger.ErrorContext(ctx, "failed to load ai integration sync schedule", attr.SlogError(err))
		return
	}
	if state.NextPollAfter.After(endTime) {
		return
	}

	if err := p.sync(ctx, cfg, state.WatermarkAt, source, endTime); err != nil {
		if classifyErr != nil {
			err = classifyErr(err)
		}
		logger.WarnContext(ctx, "ai integration sync schedule failed", attr.SlogError(err))
		if recordErr := p.store.RecordSchedulePollFailure(ctx, cfg.ID, p.schedule, endTime, p.pollInterval, err); recordErr != nil {
			logger.ErrorContext(ctx, "failed to record ai integration sync schedule poll failure", attr.SlogError(recordErr))
		}
		return
	}

	if err := p.store.RecordSchedulePollSuccess(ctx, cfg.ID, p.schedule, endTime, p.pollInterval); err != nil {
		logger.ErrorContext(ctx, "failed to record ai integration sync schedule poll success", attr.SlogError(err))
	}
}
