package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
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

	poller := &timeWindowPoller{
		store:           s.store,
		telemetryLogger: s.telemetryLogger,
		schedule:        ScheduleCursor,
		heartbeat:       s.heartbeat,
		initialLookback: initialUsagePollLookback,
		maxWindow:       0,
		granularity:     0,
	}
	source := &cursorUsageSource{
		client: cursorapi.New(s.guardianPolicy, cursorapi.WithAPIKey(cfg.APIKey)),
		svc:    s,
		cfg:    cfg,
	}
	return poller.sync(ctx, cfg, cfg.PollWatermarkAt, source, endTime)
}

// cursorUsageSource adapts one Cursor usage-events response page to the
// time-window poller. The stored watermark is the completed inclusive end of
// the previous window, so fetches start one millisecond past the window start.
type cursorUsageSource struct {
	client *cursorapi.Client
	svc    *UsagePollService
	cfg    Config
}

func (src *cursorUsageSource) UpperBound(_ context.Context, endTime time.Time) (time.Time, error) {
	return endTime, nil
}

func (src *cursorUsageSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (timeWindowPage, error) {
	pageNum := 1
	if pageToken != "" {
		parsed, err := strconv.Atoi(pageToken)
		if err != nil {
			return timeWindowPage{}, fmt.Errorf("parse cursor usage page %q: %w", pageToken, err)
		}
		pageNum = parsed
	}

	page, err := src.client.FetchUsageEventsPage(ctx, cursorapi.FetchUsageEventsPageParams{
		Start: start.Add(time.Millisecond),
		End:   end,
		Page:  pageNum,
	})
	if err != nil {
		return timeWindowPage{}, fmt.Errorf("fetch cursor usage events page: %w", err)
	}

	rows := make([]telemetry.LogParams, 0, len(page.Events))
	for _, event := range page.Events {
		rows = append(rows, src.svc.buildCursorUsageEvent(src.cfg, event))
	}

	nextPage := ""
	if page.HasNextPage {
		nextPage = strconv.Itoa(pageNum + 1)
	}
	return timeWindowPage{
		Rows:     rows,
		NextPage: nextPage,
		HasMore:  page.HasNextPage,
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
