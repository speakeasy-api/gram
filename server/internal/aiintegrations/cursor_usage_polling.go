package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

const (
	cursorUsageMetricsURN       = "cursor:usage:metrics"
	cursorHeartbeatInterval     = 10 * time.Second
	cursorUsageEventsBufferSize = 1500
)

type UsagePollService struct {
	guardianPolicy  *guardian.Policy
	telemetryLogger *telemetry.Logger
	heartbeat       func(ctx context.Context, page int)
}

func NewUsagePollService(telemetryLogger *telemetry.Logger, guardianPolicy *guardian.Policy, heartbeat func(ctx context.Context, page int)) *UsagePollService {
	if heartbeat == nil {
		panic("ai integration usage poll service requires heartbeat")
	}
	return &UsagePollService{
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
	}
}

// SyncCursorUsage imports usage events for the window between the stored
// watermark and endTime. On failure the returned error is a SyncError that
// accumulates every stage's failure alongside the progress the run made.
func (s *UsagePollService) SyncCursorUsage(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderCursor {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for usage polling: %s", cfg.Provider)
	}

	g, gctx := errgroup.WithContext(ctx)
	rawEvents := make(chan cursorapi.UsageEvent, cursorUsageEventsBufferSize)
	fetchDone := make(chan error, 1)
	apiClient := cursorapi.New(s.guardianPolicy, cursorapi.WithAPIKey(cfg.APIKey))

	// Cursor includes both time bounds, so advance past our stored inclusive watermark.
	startTime := cfg.PollWatermarkAt.Add(time.Millisecond)

	// Each stage writes only its own progress fields and error variable;
	// everything is read after g.Wait, which establishes the happens-before.
	progress := &CursorUsageSyncProgress{
		WindowStart: startTime,
		WindowEnd:   endTime,
		UsagePages:  0,
		UsageEvents: 0,
	}
	var fetchErr, writeErr error

	g.Go(func() error {
		defer close(rawEvents)
		fetchErr = s.fetchCursorUsageEvents(gctx, apiClient, startTime, endTime, rawEvents, progress)
		fetchDone <- fetchErr
		return fetchErr
	})

	g.Go(func() error {
		logParams := make([]telemetry.LogParams, 0)
		for event := range rawEvents {
			logParams = append(logParams, s.buildCursorUsageEvent(cfg, event))
		}

		// Skip the write when fetching failed: the fetch stage reports its
		// own failure and the whole window is re-polled on retry anyway.
		if err := <-fetchDone; err != nil {
			return nil
		}

		if err := s.writeCursorUsageTelemetry(gctx, logParams); err != nil {
			writeErr = oops.E(oops.CodeUnexpected, err, "failed to write to clickhouse")
			return writeErr
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return newSyncError("sync cursor usage", *progress,
			SyncStageError{Stage: "fetch_usage_events", Err: fetchErr},
			SyncStageError{Stage: "write_telemetry", Err: writeErr},
		)
	}
	return nil
}

// fetchCursorUsageEvents pages through the usage events API for the window
// and streams events to out, pausing on rate limits.
func (s *UsagePollService) fetchCursorUsageEvents(ctx context.Context, apiClient *cursorapi.Client, startTime, endTime time.Time, out chan<- cursorapi.UsageEvent, progress *CursorUsageSyncProgress) error {
	for pageNum := 1; ; {
		s.heartbeat(ctx, pageNum)

		page, err := apiClient.FetchUsageEventsPage(ctx, cursorapi.FetchUsageEventsPageParams{
			Start: startTime,
			End:   endTime,
			Page:  pageNum,
		})
		if err != nil {
			var rateLimitErr *cursorapi.RateLimitError
			if errors.As(err, &rateLimitErr) {
				sleepFor := calculateCursorRateLimitSleep(rateLimitErr.RetryAfter)
				if err := s.sleep(ctx, sleepFor, pageNum); err != nil {
					return oops.E(oops.CodeUnexpected, err, "sleep after cursor rate limit")
				}
				continue
			}
			return oops.E(oops.CodeUnexpected, err, "fetch cursor usage events page")
		}
		progress.UsagePages++

		for _, event := range page.Events {
			select {
			case <-ctx.Done():
				return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
			case out <- event:
				progress.UsageEvents++
			}
		}

		if !page.HasNextPage {
			return nil
		}
		pageNum++
	}
}

func (s *UsagePollService) buildCursorUsageEvent(cfg Config, event cursorapi.UsageEvent) telemetry.LogParams {
	userEmail := conv.NormalizeEmail(event.UserEmail)

	return telemetry.LogParams{
		Timestamp: event.Timestamp,
		ToolInfo: telemetry.ToolInfo{
			Name:           "cursor",
			OrganizationID: cfg.OrganizationID,
			ProjectID:      cfg.ProjectID.String(),
			ID:             "",
			URN:            cursorUsageMetricsURN,
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo: telemetry.UserInfoByEmail(userEmail),
		Attributes: map[attr.Key]any{
			attr.EventSourceKey:                        string(telemetry.EventSourceAPI),
			attr.LogBodyKey:                            "Cursor usage metrics",
			attr.ProjectIDKey:                          cfg.ProjectID.String(),
			attr.OrganizationIDKey:                     cfg.OrganizationID,
			attr.ResourceURNKey:                        cursorUsageMetricsURN,
			attr.HookSourceKey:                         "cursor",
			attr.AIIntegrationConfigIDKey:              cfg.ID.String(),
			attr.GenAIUsageInputTokensKey:              event.TokenUsage.InputTokens,
			attr.GenAIUsageOutputTokensKey:             event.TokenUsage.OutputTokens,
			attr.GenAIUsageCacheReadInputTokensKey:     event.TokenUsage.CacheReadTokens,
			attr.GenAIUsageCacheCreationInputTokensKey: event.TokenUsage.CacheWriteTokens,
			attr.GenAIUsageCostKey:                     event.TokenUsage.TotalCents / 100,
			attr.GenAIResponseModelKey:                 event.Model,
			attr.CursorUsageEventHashKey:               generateCursorUsageEventHash(event),
			attr.CursorChargedCentsKey:                 event.ChargedCents,
		},
	}
}

func (s *UsagePollService) writeCursorUsageTelemetry(ctx context.Context, logParams []telemetry.LogParams) error {
	if len(logParams) == 0 {
		return nil
	}

	if err := s.telemetryLogger.LogBulk(ctx, logParams); err != nil {
		return oops.E(oops.CodeUnexpected, err, "insert telemetry logs")
	}
	return nil
}

func (s *UsagePollService) sleep(ctx context.Context, d time.Duration, page int) error {
	deadline := time.Now().Add(d)
	for remaining := time.Until(deadline); remaining > 0; remaining = time.Until(deadline) {
		s.heartbeat(ctx, page)
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Preserve context cancellation sentinel errors for callers.
		case <-time.After(min(remaining, cursorHeartbeatInterval)):
		}
	}
	return nil
}

func calculateCursorRateLimitSleep(retryAfter time.Duration) time.Duration {
	if retryAfter <= 0 {
		retryAfter = time.Minute
	}

	jitter := time.Duration(time.Now().UnixNano() % int64(time.Second))
	return retryAfter + jitter
}

func generateCursorUsageEventHash(event cursorapi.UsageEvent) string {
	fields := []string{
		strconv.FormatInt(event.Timestamp.UTC().UnixMilli(), 10),
		conv.NormalizeEmail(event.UserEmail),
		event.Model,
		event.Kind,
		strconv.FormatFloat(event.ChargedCents, 'f', -1, 64),
		strconv.FormatInt(event.TokenUsage.InputTokens, 10),
		strconv.FormatInt(event.TokenUsage.OutputTokens, 10),
		strconv.FormatInt(event.TokenUsage.CacheReadTokens, 10),
		strconv.FormatInt(event.TokenUsage.CacheWriteTokens, 10),
	}

	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}
