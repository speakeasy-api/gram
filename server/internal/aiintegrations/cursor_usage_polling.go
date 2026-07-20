package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

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

// SyncCursorUsage ingests the (watermark, endTime] window of Cursor usage
// events through the shared time-window poller. Cursor events are final as
// soon as they exist, so the source's upper bound is endTime, and the whole
// range is one window. The runner advances the cursor schedule's watermark
// after the write; the activity's success recording then re-asserts it along
// with the rest of the poll state.
func (s *UsagePollService) SyncCursorUsage(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderCursor {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for usage polling: %s", cfg.Provider)
	}

	runner := &poller[[]telemetry.LogParams]{
		store:       s.store,
		schedule:    ScheduleCursor,
		heartbeat:   s.heartbeat,
		processPage: s.telemetryLogger.LogBulk,
		// Cursor usage is immediately final and has no provider window limit.
		initialLookback: initialUsagePollLookback,
		maxWindow:       0,
		granularity:     0,
		// Cursor windows are end-inclusive at millisecond resolution, so
		// resumed fetches start one millisecond past the stored watermark.
		resumeOffset: time.Millisecond,
	}
	src := &cursorUsageSource{
		client: cursorapi.New(s.guardianPolicy, cursorapi.WithAPIKey(cfg.APIKey)),
		svc:    s,
		cfg:    cfg,
	}
	return runner.sync(ctx, cfg, cfg.PollWatermarkAt, src, endTime)
}

// cursorUsageSource adapts one Cursor usage-events response page to the
// time-window poller. Fetch bounds arrive ready to use: the poller's
// resumeOffset already skips resumed fetches past the stored watermark.
type cursorUsageSource struct {
	client *cursorapi.Client
	svc    *UsagePollService
	cfg    Config
}

func (src *cursorUsageSource) UpperBound(_ context.Context, endTime time.Time) (time.Time, error) {
	return endTime, nil
}

func (src *cursorUsageSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (page[[]telemetry.LogParams], error) {
	pageNum := 1
	if pageToken != "" {
		parsed, err := strconv.Atoi(pageToken)
		if err != nil {
			return page[[]telemetry.LogParams]{}, fmt.Errorf("parse cursor usage page %q: %w", pageToken, err)
		}
		pageNum = parsed
	}

	res, err := src.client.FetchUsageEventsPage(ctx, cursorapi.FetchUsageEventsPageParams{
		Start: start,
		End:   end,
		Page:  pageNum,
	})
	if err != nil {
		return page[[]telemetry.LogParams]{}, fmt.Errorf("fetch cursor usage events page: %w", err)
	}

	rows := make([]telemetry.LogParams, 0, len(res.Events))
	for _, event := range res.Events {
		rows = append(rows, src.svc.buildCursorUsageEvent(src.cfg, event))
	}

	nextPage := ""
	if res.HasNextPage {
		nextPage = strconv.Itoa(pageNum + 1)
	}
	return page[[]telemetry.LogParams]{
		Payload:  rows,
		NextPage: nextPage,
		HasMore:  res.HasNextPage,
	}, nil
}

func (src *cursorUsageSource) RetryAfter(err error) (time.Duration, bool) {
	var rateLimitErr *cursorapi.RateLimitError
	if !errors.As(err, &rateLimitErr) {
		return 0, false
	}
	return rateLimitErr.RetryAfter, true
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
