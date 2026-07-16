package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	anthropicapi "github.com/speakeasy-api/gram/server/internal/thirdparty/anthropic"
)

const (
	// claudeChatUsageMetricsURN tags Admin Analytics user_usage_report rows
	// (token counts) in telemetry_logs and claudeChatCostMetricsURN the
	// user_cost_report rows (spend). The attribute_metrics_summaries MV admits
	// both prefixes, which is how these rows enter tokens-under-management
	// billing. Deliberately distinct from "claude-code:usage" (excluded from
	// the MV as a duplicate of OTEL data).
	claudeChatUsageMetricsURN = "claude_chat:usage:metrics"
	claudeChatCostMetricsURN  = "claude_chat:cost:metrics"

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

// syncAnalytics ingests minute buckets from the config's watermark up to the
// exports' data_refreshed_at, one <=24h window at a time. data_refreshed_at
// is the finalization watermark: buckets at or beyond it are still being
// exported and cannot be relied on, so it caps every pull rather than being
// filtered client-side — everything fetched is ingested. (Anthropic finalizes
// late-arriving events for up to 30 days; restatements behind the watermark
// are deliberately not re-read for now.) The stored watermark advances after
// each window's rows are written, so a mid-sync crash re-fetches at most one
// window, and the claude_chat.event_hash on every row lets consumers dedupe
// that overlap.
func (s *AnalyticsPollService) syncAnalytics(ctx context.Context, cfg Config, state AnalyticsSyncState, endTime time.Time) error {
	client := anthropicapi.New(s.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey), anthropicapi.WithBaseURL(s.baseURL))

	finalizedBefore, err := s.fetchFinalizedBefore(ctx, client, endTime)
	if err != nil {
		return err
	}

	windowStart := state.WatermarkAt.UTC()
	if windowStart.IsZero() {
		windowStart = endTime.Add(-anthropicAnalyticsInitialLookback)
	}
	windowStart = windowStart.Truncate(time.Minute)
	desiredEnd := minTime(endTime.UTC(), finalizedBefore).Truncate(time.Minute)

	for windowStart.Before(desiredEnd) {
		windowEnd := windowStart.Add(anthropicAnalyticsMaxWindow)
		if windowEnd.After(desiredEnd) {
			windowEnd = desiredEnd
		}

		usageRows, err := s.fetchUsageWindow(ctx, client, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("fetch anthropic user usage report: %w", err)
		}
		costRows, err := s.fetchCostWindow(ctx, client, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("fetch anthropic user cost report: %w", err)
		}

		events, err := buildClaudeChatMetricRows(cfg, usageRows, costRows)
		if err != nil {
			return err
		}
		if len(events) > 0 {
			if err := s.telemetryLogger.LogBulk(ctx, events); err != nil {
				return oops.E(oops.CodeUnexpected, err, "insert anthropic analytics telemetry logs")
			}
		}

		if err := s.store.AdvanceAnalyticsPollWatermark(ctx, cfg.ID, windowEnd); err != nil {
			return fmt.Errorf("advance anthropic analytics watermark: %w", err)
		}

		windowStart = windowEnd
	}
	return nil
}

// fetchFinalizedBefore probes both report endpoints with a minimal request
// and returns the earlier of their data_refreshed_at watermarks. Both exports
// must have finalized a bucket before it is ingested, otherwise a usage row
// could land without its spend (or vice versa).
func (s *AnalyticsPollService) fetchFinalizedBefore(ctx context.Context, client *anthropicapi.Client, endTime time.Time) (time.Time, error) {
	probeEnd := endTime.UTC().Truncate(time.Minute)
	params := anthropicapi.UserAnalyticsReportParams{
		StartingAt:  probeEnd.Add(-time.Minute),
		EndingAt:    probeEnd,
		BucketWidth: anthropicAnalyticsBucketWidth,
		Products:    []string{anthropicAnalyticsProductChat},
		GroupBy:     nil,
		Limit:       1,
		Page:        "",
	}

	s.heartbeat(ctx, "analytics_finality_probe", 1)
	usagePage, err := client.GetUserUsageReport(ctx, params)
	if err != nil {
		return time.Time{}, err //nolint:wrapcheck // Preserve HTTPError for the unauthorized classification upstream.
	}
	usageRefreshedAt, err := time.Parse(time.RFC3339, usagePage.DataRefreshedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse usage report data_refreshed_at: %w", err)
	}

	costPage, err := client.GetUserCostReport(ctx, params)
	if err != nil {
		return time.Time{}, err //nolint:wrapcheck // Preserve HTTPError for the unauthorized classification upstream.
	}
	costRefreshedAt, err := time.Parse(time.RFC3339, costPage.DataRefreshedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cost report data_refreshed_at: %w", err)
	}

	return minTime(usageRefreshedAt, costRefreshedAt).UTC(), nil
}

func (s *AnalyticsPollService) fetchUsageWindow(ctx context.Context, client *anthropicapi.Client, start, end time.Time) ([]anthropicapi.UserUsageRow, error) {
	var rows []anthropicapi.UserUsageRow
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
			return nil, err //nolint:wrapcheck // Preserve HTTPError for the unauthorized classification upstream.
		}
		rows = append(rows, res.Data...)

		if !res.HasMore || res.NextPage == "" {
			return rows, nil
		}
		page = res.NextPage
	}
}

func (s *AnalyticsPollService) fetchCostWindow(ctx context.Context, client *anthropicapi.Client, start, end time.Time) ([]anthropicapi.UserCostRow, error) {
	var rows []anthropicapi.UserCostRow
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
			return nil, err //nolint:wrapcheck // Preserve HTTPError for the unauthorized classification upstream.
		}
		rows = append(rows, res.Data...)

		if !res.HasMore || res.NextPage == "" {
			return rows, nil
		}
		page = res.NextPage
	}
}

// buildClaudeChatMetricRows converts report rows into telemetry log rows: one
// claude_chat:usage:metrics row per usage-report row and one
// claude_chat:cost:metrics row per cost-report row. The two reports are
// ingested independently — no join — so spend in a bucket without token usage
// (e.g. web-search charges) is never dropped, and the summary MV sums tokens
// and cost across both row kinds.
func buildClaudeChatMetricRows(cfg Config, usageRows []anthropicapi.UserUsageRow, costRows []anthropicapi.UserCostRow) ([]telemetry.LogParams, error) {
	logParams := make([]telemetry.LogParams, 0, len(usageRows)+len(costRows))

	for _, row := range usageRows {
		bucketStart, err := row.StartingAtTime()
		if err != nil {
			return nil, fmt.Errorf("parse usage row starting_at: %w", err)
		}
		params := newClaudeChatLogParams(cfg, claudeChatUsageMetricsURN, "Claude Chat usage metrics", bucketStart, row.Actor, row.Model)
		params.Attributes[attr.GenAIUsageInputTokensKey] = row.UncachedInputTokens
		params.Attributes[attr.GenAIUsageOutputTokensKey] = row.OutputTokens
		params.Attributes[attr.GenAIUsageCacheReadInputTokensKey] = row.CacheReadInputTokens
		params.Attributes[attr.GenAIUsageCacheCreationInputTokensKey] = row.CacheCreation.Ephemeral1hInputTokens + row.CacheCreation.Ephemeral5mInputTokens
		params.Attributes[attr.ClaudeChatEventHashKey] = generateClaudeChatUsageRowHash(row)
		logParams = append(logParams, params)
	}

	for _, row := range costRows {
		bucketStart, err := time.Parse(time.RFC3339, row.StartingAt)
		if err != nil {
			return nil, fmt.Errorf("parse cost row starting_at: %w", err)
		}
		amountUSD, err := row.AmountUSD()
		if err != nil {
			return nil, fmt.Errorf("convert analytics cost row amount: %w", err)
		}
		params := newClaudeChatLogParams(cfg, claudeChatCostMetricsURN, "Claude Chat cost metrics", bucketStart.UTC(), row.Actor, row.Model)
		params.Attributes[attr.GenAIUsageCostKey] = amountUSD
		params.Attributes[attr.ClaudeChatEventHashKey] = generateClaudeChatCostRowHash(row)
		logParams = append(logParams, params)
	}

	return logParams, nil
}

// Report rows carry no upstream identifier, but a finalized 1m bucket is
// deterministic: re-fetching the same window yields byte-identical rows. The
// hashes below fingerprint a row's aggregation key and values so consumers
// needing exact-once sums can dedupe by (gram_project_id,
// claude_chat.event_hash) — the same insurance cursor.event_hash provides —
// covering the crash window between the ClickHouse write and the watermark
// advance, where one window can be re-ingested.

func generateClaudeChatUsageRowHash(row anthropicapi.UserUsageRow) string {
	fields := []string{
		"usage",
		row.StartingAt,
		row.Actor.UserID,
		row.Model,
		row.Product,
		strconv.FormatInt(row.UncachedInputTokens, 10),
		strconv.FormatInt(row.OutputTokens, 10),
		strconv.FormatInt(row.CacheReadInputTokens, 10),
		strconv.FormatInt(row.CacheCreation.Ephemeral1hInputTokens, 10),
		strconv.FormatInt(row.CacheCreation.Ephemeral5mInputTokens, 10),
		strconv.FormatInt(row.Requests, 10),
	}

	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}

func generateClaudeChatCostRowHash(row anthropicapi.UserCostRow) string {
	fields := []string{
		"cost",
		row.StartingAt,
		row.Actor.UserID,
		row.Model,
		row.Product,
		row.Amount,
		row.Currency,
		strconv.FormatInt(row.Requests, 10),
	}

	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}

// newClaudeChatLogParams stamps the provenance shared by usage and cost rows:
// URN, hook source, account/provider classification, org/config attribution,
// and the actor's identity.
func newClaudeChatLogParams(cfg Config, urn string, body string, bucketStart time.Time, actor anthropicapi.AnalyticsActor, model string) telemetry.LogParams {
	attrs := map[attr.Key]any{
		attr.EventSourceKey:           string(telemetry.EventSourceAPI),
		attr.LogBodyKey:               body,
		attr.ProjectIDKey:             cfg.ProjectID.String(),
		attr.OrganizationIDKey:        cfg.OrganizationID,
		attr.ResourceURNKey:           urn,
		attr.HookSourceKey:            anthropicAnalyticsHookSource,
		attr.AIIntegrationConfigIDKey: cfg.ID.String(),
		attr.ProviderKey:              anthropicAnalyticsProviderTag,
		attr.AccountTypeKey:           anthropicAnalyticsAccountType,
	}
	if model != "" {
		attrs[attr.GenAIResponseModelKey] = model
	}
	if actor.UserID != "" {
		attrs[attr.ExternalUserIDKey] = actor.UserID
	}
	if cfg.ExternalOrganizationID != nil && *cfg.ExternalOrganizationID != "" {
		attrs[attr.ExternalOrgIDKey] = *cfg.ExternalOrganizationID
	}
	if cfg.BillingMode != "" {
		attrs[attr.BillingModeKey] = cfg.BillingMode
	}

	return telemetry.LogParams{
		Timestamp: bucketStart,
		ToolInfo: telemetry.ToolInfo{
			Name:           anthropicAnalyticsHookSource,
			OrganizationID: cfg.OrganizationID,
			ProjectID:      cfg.ProjectID.String(),
			ID:             "",
			URN:            urn,
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByEmail(conv.NormalizeEmail(conv.PtrValOr(actor.Email, ""))),
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
