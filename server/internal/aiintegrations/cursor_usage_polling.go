package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	cursorapi "github.com/speakeasy-api/gram/server/internal/thirdparty/cursor"
)

const cursorUsageMetricsURN = "cursor:usage:metrics"

type UsagePollService struct {
	store           *Store
	guardianPolicy  *guardian.Policy
	telemetryLogger *telemetry.Logger
	heartbeat       func(ctx context.Context, page int)
}

func NewUsagePollService(store *Store, telemetryLogger *telemetry.Logger, guardianPolicy *guardian.Policy, heartbeat func(ctx context.Context, page int)) *UsagePollService {
	if heartbeat == nil {
		panic("ai integration usage poll service requires heartbeat")
	}
	return &UsagePollService{
		store:           store,
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
	}
}

// SyncCursorUsage ingests the Cursor usage window through the shared
// time-window poller.
func (s *UsagePollService) SyncCursorUsage(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderCursor {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for usage polling: %s", cfg.Provider)
	}

	source, err := NewCursorUsageSource(s.guardianPolicy, cfg, s.telemetryLogger.LogBulk, "", 0)
	if err != nil {
		return fmt.Errorf("build cursor usage source: %w", err)
	}

	runner := &timewindowpoller.Poller[[]telemetry.LogParams]{
		Store:    s.store,
		Schedule: ScheduleCursor,
		State: timewindowpoller.SyncState{
			SyncID:      cfg.SyncID,
			WatermarkAt: cfg.PollWatermarkAt,
			Checkpoint:  cfg.PollCheckpoint,
		},
		Source:  source,
		EndTime: endTime,
		Heartbeat: func(ctx context.Context, page int) {
			s.heartbeat(ctx, page)
		},
		InitialLookback: InitialUsagePollLookback,
		MaxWindow:       0,
		Granularity:     0,
		ResumeOffset:    time.Millisecond,
	}
	if err := runner.Do(ctx); err != nil {
		return fmt.Errorf("sync cursor usage: %w", err)
	}
	return nil
}

func NewCursorUsageSource(
	guardianPolicy *guardian.Policy,
	cfg Config,
	processPage func(ctx context.Context, payload []telemetry.LogParams) error,
	baseURL string,
	pageLimit int,
) (timewindowpoller.Source[[]telemetry.LogParams], error) {
	if guardianPolicy == nil {
		return nil, fmt.Errorf("guardian policy is required")
	}
	if processPage == nil {
		return nil, fmt.Errorf("process page is required")
	}
	if cfg.Provider == "" {
		cfg.Provider = ProviderCursor
	}
	if cfg.Provider != ProviderCursor {
		return nil, fmt.Errorf("schedule %q requires provider %q, got %q", ScheduleCursor, ProviderCursor, cfg.Provider)
	}

	client := cursorapi.New(
		guardianPolicy,
		cursorapi.WithAPIKey(cfg.APIKey),
		cursorapi.WithBaseURL(baseURL),
		cursorapi.WithPageSize(pageLimit),
	)
	return &cursorUsageSource{
		client:      client,
		cfg:         cfg,
		processPage: processPage,
	}, nil
}

// cursorUsageSource adapts one Cursor usage-events response page to the
// time-window poller. Fetch bounds arrive ready to use: the poller's
// resumeOffset already skips resumed fetches past the stored watermark.
type cursorUsageSource struct {
	client      *cursorapi.Client
	cfg         Config
	processPage func(ctx context.Context, payload []telemetry.LogParams) error
}

func (src *cursorUsageSource) UpperBound(_ context.Context, endTime time.Time) (time.Time, error) {
	return endTime, nil
}

func (src *cursorUsageSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (timewindowpoller.Page[[]telemetry.LogParams], error) {
	pageNum := 1
	if pageToken != "" {
		parsed, err := strconv.Atoi(pageToken)
		if err != nil {
			return timewindowpoller.Page[[]telemetry.LogParams]{Payload: nil, NextPage: "", HasMore: false}, fmt.Errorf("parse cursor usage page %q: %w", pageToken, err)
		}
		pageNum = parsed
	}

	res, err := src.client.FetchUsageEventsPage(ctx, cursorapi.FetchUsageEventsPageParams{
		Start: start,
		End:   end,
		Page:  pageNum,
	})
	if err != nil {
		return timewindowpoller.Page[[]telemetry.LogParams]{Payload: nil, NextPage: "", HasMore: false}, fmt.Errorf("fetch cursor usage events page: %w", err)
	}

	rows := make([]telemetry.LogParams, 0, len(res.Events))
	for _, event := range res.Events {
		rows = append(rows, buildCursorUsageEvent(src.cfg, event))
	}

	nextPage := ""
	if res.HasNextPage {
		nextPage = strconv.Itoa(pageNum + 1)
	}
	return timewindowpoller.Page[[]telemetry.LogParams]{
		Payload:  rows,
		NextPage: nextPage,
		HasMore:  res.HasNextPage,
	}, nil
}

func (src *cursorUsageSource) ProcessPage(ctx context.Context, payload []telemetry.LogParams) error {
	return src.processPage(ctx, payload)
}

func (src *cursorUsageSource) RetryAfter(err error) (time.Duration, bool) {
	var rateLimitErr *cursorapi.RateLimitError
	if !errors.As(err, &rateLimitErr) {
		return 0, false
	}
	return rateLimitErr.RetryAfter, true
}

func buildCursorUsageEvent(cfg Config, event cursorapi.UsageEvent) telemetry.LogParams {
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

func generateCursorUsageEventHash(event cursorapi.UsageEvent) string {
	return eventKey{
		event.Timestamp,
		conv.NormalizeEmail(event.UserEmail),
		event.Model,
		event.Kind,
		event.ChargedCents,
		event.TokenUsage.InputTokens,
		event.TokenUsage.OutputTokens,
		event.TokenUsage.CacheReadTokens,
		event.TokenUsage.CacheWriteTokens,
	}.hash()
}
