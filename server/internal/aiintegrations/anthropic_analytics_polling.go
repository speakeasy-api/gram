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

// anthropicAnalyticsSourceFactory builds the report-specific source for one
// sync, bound to that sync's API client and config.
type anthropicAnalyticsSourceFactory func(client *anthropicapi.Client, cfg Config, heartbeat func(ctx context.Context, scope string, page int)) timeWindowSource

// AnthropicAnalyticsPoller ingests one Admin Analytics report — usage or
// cost — from the Anthropic API into telemetry_logs, where the
// attribute_metrics_summaries MV picks it up for tokens-under-management
// billing. Each report is its own service instance driving its own sync
// schedule: construct one with NewAnthropicUsageAnalyticsPoller and one with
// NewAnthropicCostAnalyticsPoller. Usage is ingested per
// (user, minute, model) without any session linkage — the Compliance API
// import owns session content.
type AnthropicAnalyticsPoller struct {
	logger          *slog.Logger
	store           *Store
	guardianPolicy  *guardian.Policy
	telemetryLogger *telemetry.Logger
	heartbeat       func(ctx context.Context, scope string, page int)
	schedule        string
	newSource       anthropicAnalyticsSourceFactory
	// baseURL overrides the Anthropic API base URL in tests; empty means the
	// client default.
	baseURL string
}

// NewAnthropicUsageAnalyticsPoller ingests the user_usage_report (token
// counts) as the anthropic_analytics_usage schedule.
func NewAnthropicUsageAnalyticsPoller(
	logger *slog.Logger,
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, scope string, page int),
) *AnthropicAnalyticsPoller {
	return newAnthropicAnalyticsPoller(logger, store, guardianPolicy, telemetryLogger, heartbeat, ScheduleAnthropicAnalyticsUsage,
		func(client *anthropicapi.Client, cfg Config, heartbeat func(ctx context.Context, scope string, page int)) timeWindowSource {
			return &anthropicUsageReportSource{client: client, cfg: cfg, heartbeat: heartbeat}
		})
}

// NewAnthropicCostAnalyticsPoller ingests the user_cost_report (spend) as
// the anthropic_analytics_cost schedule.
func NewAnthropicCostAnalyticsPoller(
	logger *slog.Logger,
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, scope string, page int),
) *AnthropicAnalyticsPoller {
	return newAnthropicAnalyticsPoller(logger, store, guardianPolicy, telemetryLogger, heartbeat, ScheduleAnthropicAnalyticsCost,
		func(client *anthropicapi.Client, cfg Config, heartbeat func(ctx context.Context, scope string, page int)) timeWindowSource {
			return &anthropicCostReportSource{client: client, cfg: cfg, heartbeat: heartbeat}
		})
}

func newAnthropicAnalyticsPoller(
	logger *slog.Logger,
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, scope string, page int),
	schedule string,
	newSource anthropicAnalyticsSourceFactory,
) *AnthropicAnalyticsPoller {
	if heartbeat == nil {
		panic("ai integration analytics poller requires heartbeat")
	}
	return &AnthropicAnalyticsPoller{
		logger:          logger.With(attr.SlogComponent("aiintegrations.anthropic_analytics")),
		store:           store,
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
		schedule:        schedule,
		newSource:       newSource,
		baseURL:         "",
	}
}

// MaybeSync runs the report's schedule when due and records the outcome on
// its own sync row. Failures are recorded and logged but never returned:
// analytics ingestion must not block the compliance import sharing the poll
// activity, and each report fails independently of the other. Non-Enterprise
// organizations (or keys without the read:analytics scope) get a 403 from
// the API and simply accumulate failure state until access is granted.
func (p *AnthropicAnalyticsPoller) MaybeSync(ctx context.Context, cfg Config, endTime time.Time) {
	if cfg.Provider != ProviderAnthropicCompliance {
		return
	}

	logger := p.logger.With(
		attr.SlogOrganizationID(cfg.OrganizationID),
		attr.SlogAIIntegrationConfigID(cfg.ID.String()),
	)
	client := anthropicapi.New(p.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey), anthropicapi.WithBaseURL(p.baseURL))

	runner := &timeWindowPoller{
		store:           p.store,
		telemetryLogger: p.telemetryLogger,
		schedule:        p.schedule,
		pollInterval:    anthropicAnalyticsPollInterval,
		initialLookback: anthropicAnalyticsInitialLookback,
		maxWindow:       anthropicAnalyticsMaxWindow,
		granularity:     time.Minute,
	}
	runner.maybeSync(ctx, logger, cfg, p.newSource(client, cfg, p.heartbeat), endTime, classifyAnthropicAnalyticsErr)
}

// classifyAnthropicAnalyticsErr rewrites auth failures so the stored poll
// error explains the plan/scope requirement instead of a bare status code.
func classifyAnthropicAnalyticsErr(err error) error {
	var httpErr *anthropicapi.HTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden) {
		return fmt.Errorf("anthropic analytics access denied - the analytics api requires a claude enterprise plan and an api key with the read:analytics scope: %w", err)
	}
	return err
}

// analyticsProbeParams is a minimal single-bucket request used only to read a
// report's data_refreshed_at finalization watermark.
func analyticsProbeParams(endTime time.Time) anthropicapi.UserAnalyticsReportParams {
	probeEnd := endTime.UTC().Truncate(time.Minute)
	return anthropicapi.UserAnalyticsReportParams{
		StartingAt:  probeEnd.Add(-time.Minute),
		EndingAt:    probeEnd,
		BucketWidth: anthropicAnalyticsBucketWidth,
		Products:    []string{anthropicAnalyticsProductChat},
		GroupBy:     nil,
		Limit:       1,
		Page:        "",
	}
}

func analyticsWindowParams(start, end time.Time, page string) anthropicapi.UserAnalyticsReportParams {
	return anthropicapi.UserAnalyticsReportParams{
		StartingAt:  start,
		EndingAt:    end,
		BucketWidth: anthropicAnalyticsBucketWidth,
		Products:    []string{anthropicAnalyticsProductChat},
		GroupBy:     []string{"model"},
		Limit:       anthropicAnalyticsPageLimit,
		Page:        page,
	}
}

// anthropicUsageReportSource pulls the user_usage_report for the
// anthropic_analytics_usage schedule. data_refreshed_at is the report's
// finalization watermark — buckets at or beyond it are still being exported
// and cannot be relied on — so it caps every pull. (Anthropic finalizes
// late-arriving events for up to 30 days; restatements behind the watermark
// are deliberately not re-read for now.)
type anthropicUsageReportSource struct {
	client    *anthropicapi.Client
	cfg       Config
	heartbeat func(ctx context.Context, scope string, page int)
}

func (src *anthropicUsageReportSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	src.heartbeat(ctx, "analytics_usage_probe", 1)
	page, err := src.client.GetUserUsageReport(ctx, analyticsProbeParams(endTime))
	if err != nil {
		return time.Time{}, err //nolint:wrapcheck // Preserve HTTPError for the access-denied classification upstream.
	}
	refreshedAt, err := time.Parse(time.RFC3339, page.DataRefreshedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse usage report data_refreshed_at: %w", err)
	}
	return refreshedAt.UTC(), nil
}

func (src *anthropicUsageReportSource) FetchWindow(ctx context.Context, start, end time.Time) ([]telemetry.LogParams, error) {
	var rows []anthropicapi.UserUsageRow
	page := ""
	for pageNum := 1; ; pageNum++ {
		src.heartbeat(ctx, "analytics_usage", pageNum)
		res, err := src.client.GetUserUsageReport(ctx, analyticsWindowParams(start, end, page))
		if err != nil {
			return nil, err //nolint:wrapcheck // Preserve HTTPError for the access-denied classification upstream.
		}
		rows = append(rows, res.Data...)

		if !res.HasMore || res.NextPage == "" {
			return buildClaudeChatUsageRows(src.cfg, rows)
		}
		page = res.NextPage
	}
}

// anthropicCostReportSource pulls the user_cost_report for the
// anthropic_analytics_cost schedule, mirroring the usage source.
type anthropicCostReportSource struct {
	client    *anthropicapi.Client
	cfg       Config
	heartbeat func(ctx context.Context, scope string, page int)
}

func (src *anthropicCostReportSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	src.heartbeat(ctx, "analytics_cost_probe", 1)
	page, err := src.client.GetUserCostReport(ctx, analyticsProbeParams(endTime))
	if err != nil {
		return time.Time{}, err //nolint:wrapcheck // Preserve HTTPError for the access-denied classification upstream.
	}
	refreshedAt, err := time.Parse(time.RFC3339, page.DataRefreshedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cost report data_refreshed_at: %w", err)
	}
	return refreshedAt.UTC(), nil
}

func (src *anthropicCostReportSource) FetchWindow(ctx context.Context, start, end time.Time) ([]telemetry.LogParams, error) {
	var rows []anthropicapi.UserCostRow
	page := ""
	for pageNum := 1; ; pageNum++ {
		src.heartbeat(ctx, "analytics_cost", pageNum)
		res, err := src.client.GetUserCostReport(ctx, analyticsWindowParams(start, end, page))
		if err != nil {
			return nil, err //nolint:wrapcheck // Preserve HTTPError for the access-denied classification upstream.
		}
		rows = append(rows, res.Data...)

		if !res.HasMore || res.NextPage == "" {
			return buildClaudeChatCostRows(src.cfg, rows)
		}
		page = res.NextPage
	}
}

// buildClaudeChatUsageRows converts usage-report rows into
// claude_chat:usage:metrics telemetry rows. Usage and cost are ingested
// independently — no join — so spend in a bucket without token usage (e.g.
// web-search charges) is never dropped, and the summary MV sums tokens and
// cost across both row kinds.
func buildClaudeChatUsageRows(cfg Config, rows []anthropicapi.UserUsageRow) ([]telemetry.LogParams, error) {
	logParams := make([]telemetry.LogParams, 0, len(rows))
	for _, row := range rows {
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
	return logParams, nil
}

// buildClaudeChatCostRows converts cost-report rows into
// claude_chat:cost:metrics telemetry rows.
func buildClaudeChatCostRows(cfg Config, rows []anthropicapi.UserCostRow) ([]telemetry.LogParams, error) {
	logParams := make([]telemetry.LogParams, 0, len(rows))
	for _, row := range rows {
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
