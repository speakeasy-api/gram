package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
)

const (
	// anthropicUsageMetricsURN tags Admin Analytics API usage rows in
	// telemetry_logs. The attribute_metrics_summaries MV admits rows whose URN
	// starts with "anthropic:usage", which is how these rows enter
	// tokens-under-management billing. Deliberately distinct from
	// "claude-code:usage" (excluded from the MV as a duplicate of OTEL data).
	anthropicUsageMetricsURN = "anthropic:usage:metrics"

	// anthropicAnalyticsHookSource is the consuming-surface tag for Claude
	// Chat (web + desktop) usage observed via the Admin Analytics API. It must
	// never collide with "claude-code": that surface is already counted from
	// the Claude Code OTEL stream and would double bill.
	anthropicAnalyticsHookSource = "claude-chat"

	// anthropicAnalyticsProductChat is the Admin Analytics product surface for
	// Claude Chat web and desktop. Restricting to it keeps claude_code and
	// cowork usage (observed through other pipelines) out of this path.
	anthropicAnalyticsProductChat = "chat"

	// anthropicAnalyticsBucketWidth requests per-minute buckets. The API caps
	// a 1m-bucket request range at 24 hours, hence the window chunking.
	anthropicAnalyticsBucketWidth = "1m"
	anthropicAnalyticsMaxWindow   = 24 * time.Hour
	anthropicAnalyticsPageLimit   = 1000

	anthropicAnalyticsProviderTag = "anthropic"
	// anthropicAnalyticsAccountType classifies analytics rows as team
	// accounts: the Admin Analytics API only reports seat users of the
	// organization's Claude Enterprise plan.
	anthropicAnalyticsAccountType = "team"
)

// AnalyticsPollService ingests per-user Claude Chat token usage and cost from
// the Anthropic Admin Analytics API into telemetry_logs, where the
// attribute_metrics_summaries MV picks it up for tokens-under-management
// billing. Usage is ingested per (user, minute, model) without any session
// linkage — the Compliance API import owns session content.
type AnalyticsPollService struct {
	logger          *slog.Logger
	store           *Store
	guardianPolicy  *guardian.Policy
	telemetryLogger *telemetry.Logger
	heartbeat       func(ctx context.Context, scope string, page int)
	// baseURL overrides the Anthropic API base URL in tests; empty means the
	// client default.
	baseURL string
}

func NewAnalyticsPollService(
	logger *slog.Logger,
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, scope string, page int),
) *AnalyticsPollService {
	if heartbeat == nil {
		panic("ai integration analytics poll service requires heartbeat")
	}
	return &AnalyticsPollService{
		logger:          logger.With(attr.SlogComponent("aiintegrations.anthropic_analytics")),
		store:           store,
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
		baseURL:         "",
	}
}

// MaybeSyncAnthropicAnalytics runs an analytics sync when one is due and
// records the outcome on the config's analytics sync state. Failures are
// recorded and logged but never returned: analytics ingestion must not block
// the compliance import sharing the poll activity. Non-Enterprise
// organizations (or keys without the read:analytics scope) get a 403 from the
// API and simply accumulate failure state until access is granted.
func (s *AnalyticsPollService) MaybeSyncAnthropicAnalytics(ctx context.Context, cfg Config, endTime time.Time) {
	if cfg.Provider != ProviderAnthropicCompliance {
		return
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(cfg.OrganizationID),
		attr.SlogAIIntegrationConfigID(cfg.ID.String()),
	)

	state, err := s.store.EnsureAnalyticsSync(ctx, cfg.ID)
	if err != nil {
		logger.ErrorContext(ctx, "failed to load anthropic analytics sync state", attr.SlogError(err))
		return
	}
	if state.NextPollAfter.After(endTime) {
		return
	}

	if err := s.syncAnalytics(ctx, cfg, state, endTime); err != nil {
		var httpErr *anthropicapi.HTTPError
		if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden) {
			err = fmt.Errorf("anthropic analytics access denied - the analytics api requires a claude enterprise plan and an api key with the read:analytics scope: %w", err)
		}
		logger.WarnContext(ctx, "anthropic analytics sync failed", attr.SlogError(err))
		if recordErr := s.store.RecordAnalyticsPollFailure(ctx, cfg.ID, endTime, err); recordErr != nil {
			logger.ErrorContext(ctx, "failed to record anthropic analytics poll failure", attr.SlogError(recordErr))
		}
		return
	}

	if err := s.store.RecordAnalyticsPollSuccess(ctx, cfg.ID, endTime); err != nil {
		logger.ErrorContext(ctx, "failed to record anthropic analytics poll success", attr.SlogError(err))
	}
}

// syncAnalytics ingests complete minute buckets from the config's watermark up
// to the API's data-refresh watermark, one <=24h window at a time. The stored
// watermark advances after each window's rows are written, so a mid-sync crash
// re-fetches at most one window. Buckets at or beyond data_refreshed_at are
// left for the next poll: the export only re-runs every ~4 hours, and
// ingesting a bucket exactly once (when it is final) is what keeps billing
// sums accurate without a reconciliation pass.
func (s *AnalyticsPollService) syncAnalytics(ctx context.Context, cfg Config, state AnalyticsSyncState, endTime time.Time) error {
	client := anthropicapi.New(s.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey), anthropicapi.WithBaseURL(s.baseURL))

	windowStart := state.WatermarkAt.UTC()
	if windowStart.IsZero() {
		windowStart = endTime.Add(-anthropicAnalyticsInitialLookback)
	}
	windowStart = windowStart.Truncate(time.Minute)
	desiredEnd := endTime.UTC().Truncate(time.Minute)

	for windowStart.Before(desiredEnd) {
		windowEnd := windowStart.Add(anthropicAnalyticsMaxWindow)
		if windowEnd.After(desiredEnd) {
			windowEnd = desiredEnd
		}

		usageRows, usageRefreshedAt, err := s.fetchUsageWindow(ctx, client, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("fetch anthropic user usage report: %w", err)
		}
		costRows, costRefreshedAt, err := s.fetchCostWindow(ctx, client, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("fetch anthropic user cost report: %w", err)
		}

		// Only buckets that ended before both exports' refresh watermarks are
		// final enough to ingest. Everything at or beyond the cutoff is
		// deferred to the next poll.
		cutoff := minTime(usageRefreshedAt, costRefreshedAt).UTC().Truncate(time.Minute)
		if cutoff.After(windowEnd) {
			cutoff = windowEnd
		}
		if !cutoff.After(windowStart) {
			return nil
		}

		events, err := buildAnthropicUsageEvents(cfg, usageRows, costRows, cutoff)
		if err != nil {
			return err
		}
		if len(events) > 0 {
			if err := s.telemetryLogger.LogBulk(ctx, events); err != nil {
				return oops.E(oops.CodeUnexpected, err, "insert anthropic analytics telemetry logs")
			}
		}

		if err := s.store.AdvanceAnalyticsPollWatermark(ctx, cfg.ID, cutoff); err != nil {
			return fmt.Errorf("advance anthropic analytics watermark: %w", err)
		}

		if cutoff.Before(windowEnd) {
			// The refresh watermark truncated this window; later windows are
			// entirely beyond it.
			return nil
		}
		windowStart = windowEnd
	}
	return nil
}

func (s *AnalyticsPollService) fetchUsageWindow(ctx context.Context, client *anthropicapi.Client, start, end time.Time) ([]anthropicapi.UserUsageRow, time.Time, error) {
	var rows []anthropicapi.UserUsageRow
	refreshedAt := time.Time{}
	page := ""
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "analytics_usage", pageNum)
		res, err := client.GetUserUsageReport(ctx, anthropicapi.UserAnalyticsReportParams{
			StartingAt:  start,
			EndingAt:    end,
			BucketWidth: anthropicAnalyticsBucketWidth,
			Products:    []string{anthropicAnalyticsProductChat},
			GroupBy:     []string{"model"},
			Limit:       anthropicAnalyticsPageLimit,
			Page:        page,
		})
		if err != nil {
			return nil, time.Time{}, err //nolint:wrapcheck // Preserve HTTPError for the unauthorized classification upstream.
		}
		rows = append(rows, res.Data...)

		pageRefreshedAt, err := time.Parse(time.RFC3339, res.DataRefreshedAt)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("parse usage report data_refreshed_at: %w", err)
		}
		refreshedAt = minTime(refreshedAt, pageRefreshedAt)

		if !res.HasMore || res.NextPage == "" {
			return rows, refreshedAt, nil
		}
		page = res.NextPage
	}
}

func (s *AnalyticsPollService) fetchCostWindow(ctx context.Context, client *anthropicapi.Client, start, end time.Time) ([]anthropicapi.UserCostRow, time.Time, error) {
	var rows []anthropicapi.UserCostRow
	refreshedAt := time.Time{}
	page := ""
	for pageNum := 1; ; pageNum++ {
		s.heartbeat(ctx, "analytics_cost", pageNum)
		res, err := client.GetUserCostReport(ctx, anthropicapi.UserAnalyticsReportParams{
			StartingAt:  start,
			EndingAt:    end,
			BucketWidth: anthropicAnalyticsBucketWidth,
			Products:    []string{anthropicAnalyticsProductChat},
			GroupBy:     []string{"model"},
			Limit:       anthropicAnalyticsPageLimit,
			Page:        page,
		})
		if err != nil {
			return nil, time.Time{}, err //nolint:wrapcheck // Preserve HTTPError for the unauthorized classification upstream.
		}
		rows = append(rows, res.Data...)

		pageRefreshedAt, err := time.Parse(time.RFC3339, res.DataRefreshedAt)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("parse cost report data_refreshed_at: %w", err)
		}
		refreshedAt = minTime(refreshedAt, pageRefreshedAt)

		if !res.HasMore || res.NextPage == "" {
			return rows, refreshedAt, nil
		}
		page = res.NextPage
	}
}

// anthropicUsageEventKey joins usage and cost rows reported for the same
// actor, minute bucket, and model.
type anthropicUsageEventKey struct {
	userID     string
	startingAt string
	model      string
}

// anthropicUsageEvent is one joined usage/cost data point ready to emit.
type anthropicUsageEvent struct {
	bucketStart          time.Time
	email                string
	externalUserID       string
	model                string
	uncachedInputTokens  int64
	outputTokens         int64
	cacheReadInputTokens int64
	cacheCreationTokens  int64
	costUSD              float64
}

// buildAnthropicUsageEvents joins usage rows with cost rows on (actor, bucket,
// model) and converts complete buckets (starting before cutoff) into telemetry
// log rows. Cost rows without a usage counterpart (e.g. web-search charges in
// a minute with no token usage row) still produce a cost-only event so spend
// is never dropped.
func buildAnthropicUsageEvents(cfg Config, usageRows []anthropicapi.UserUsageRow, costRows []anthropicapi.UserCostRow, cutoff time.Time) ([]telemetry.LogParams, error) {
	events := make(map[anthropicUsageEventKey]*anthropicUsageEvent, len(usageRows))
	order := make([]anthropicUsageEventKey, 0, len(usageRows))

	upsert := func(key anthropicUsageEventKey, email *string) (*anthropicUsageEvent, error) {
		if event, ok := events[key]; ok {
			return event, nil
		}
		bucketStart, err := time.Parse(time.RFC3339, key.startingAt)
		if err != nil {
			return nil, fmt.Errorf("parse analytics row starting_at: %w", err)
		}
		event := &anthropicUsageEvent{
			bucketStart:          bucketStart.UTC(),
			email:                conv.NormalizeEmail(conv.PtrValOr(email, "")),
			externalUserID:       key.userID,
			model:                key.model,
			uncachedInputTokens:  0,
			outputTokens:         0,
			cacheReadInputTokens: 0,
			cacheCreationTokens:  0,
			costUSD:              0,
		}
		events[key] = event
		order = append(order, key)
		return event, nil
	}

	for _, row := range usageRows {
		event, err := upsert(anthropicUsageEventKey{
			userID:     row.Actor.UserID,
			startingAt: row.StartingAt,
			model:      row.Model,
		}, row.Actor.Email)
		if err != nil {
			return nil, err
		}
		event.uncachedInputTokens += row.UncachedInputTokens
		event.outputTokens += row.OutputTokens
		event.cacheReadInputTokens += row.CacheReadInputTokens
		event.cacheCreationTokens += row.CacheCreation.Ephemeral1hInputTokens + row.CacheCreation.Ephemeral5mInputTokens
	}

	for _, row := range costRows {
		event, err := upsert(anthropicUsageEventKey{
			userID:     row.Actor.UserID,
			startingAt: row.StartingAt,
			model:      row.Model,
		}, row.Actor.Email)
		if err != nil {
			return nil, err
		}
		amountUSD, err := row.AmountUSD()
		if err != nil {
			return nil, fmt.Errorf("convert analytics cost row amount: %w", err)
		}
		event.costUSD += amountUSD
	}

	logParams := make([]telemetry.LogParams, 0, len(order))
	for _, key := range order {
		event := events[key]
		if !event.bucketStart.Before(cutoff) {
			continue
		}
		logParams = append(logParams, buildAnthropicUsageLogParams(cfg, event))
	}
	return logParams, nil
}

func buildAnthropicUsageLogParams(cfg Config, event *anthropicUsageEvent) telemetry.LogParams {
	attrs := map[attr.Key]any{
		attr.EventSourceKey:                        string(telemetry.EventSourceAPI),
		attr.LogBodyKey:                            "Anthropic chat usage metrics",
		attr.ProjectIDKey:                          cfg.ProjectID.String(),
		attr.OrganizationIDKey:                     cfg.OrganizationID,
		attr.ResourceURNKey:                        anthropicUsageMetricsURN,
		attr.HookSourceKey:                         anthropicAnalyticsHookSource,
		attr.AIIntegrationConfigIDKey:              cfg.ID.String(),
		attr.GenAIUsageInputTokensKey:              event.uncachedInputTokens,
		attr.GenAIUsageOutputTokensKey:             event.outputTokens,
		attr.GenAIUsageCacheReadInputTokensKey:     event.cacheReadInputTokens,
		attr.GenAIUsageCacheCreationInputTokensKey: event.cacheCreationTokens,
		attr.GenAIUsageCostKey:                     event.costUSD,
		attr.ProviderKey:                           anthropicAnalyticsProviderTag,
		attr.AccountTypeKey:                        anthropicAnalyticsAccountType,
	}
	if event.model != "" {
		attrs[attr.GenAIResponseModelKey] = event.model
	}
	if event.externalUserID != "" {
		attrs[attr.ExternalUserIDKey] = event.externalUserID
	}
	if cfg.ExternalOrganizationID != nil && *cfg.ExternalOrganizationID != "" {
		attrs[attr.ExternalOrgIDKey] = *cfg.ExternalOrganizationID
	}
	if cfg.BillingMode != "" {
		attrs[attr.BillingModeKey] = cfg.BillingMode
	}

	return telemetry.LogParams{
		Timestamp: event.bucketStart,
		ToolInfo: telemetry.ToolInfo{
			Name:           anthropicAnalyticsHookSource,
			OrganizationID: cfg.OrganizationID,
			ProjectID:      cfg.ProjectID.String(),
			ID:             "",
			URN:            anthropicUsageMetricsURN,
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByEmail(event.email),
		Attributes: attrs,
	}
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
