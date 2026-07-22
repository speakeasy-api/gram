package aiintegrations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/timewindowpoller"
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
	// user_cost_report rows (spend). The shared attribute metrics summary
	// retains both prefixes for analytics and TUM billing.
	// Deliberately distinct from "claude-code:usage", which is excluded from
	// observed-traffic billing as a duplicate of OTEL data.
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

const (
	AnthropicAnalyticsMaxWindow = anthropicAnalyticsMaxWindow
	AnthropicAnalyticsPageLimit = anthropicAnalyticsPageLimit
)

type anthropicAnalyticsSourceFactory func(
	guardianPolicy *guardian.Policy,
	cfg Config,
	processPage func(ctx context.Context, payload []telemetry.LogParams) error,
	baseURL string,
	pageLimit int,
) (timewindowpoller.Source[[]telemetry.LogParams], error)

// AnthropicAnalyticsPoller ingests one Admin Analytics report — usage or cost
// — from the Anthropic API into telemetry_logs.
type AnthropicAnalyticsPoller struct {
	store           *Store
	guardianPolicy  *guardian.Policy
	telemetryLogger *telemetry.Logger
	heartbeat       func(ctx context.Context, schedule string, page int)
	schedule        string
	newSource       anthropicAnalyticsSourceFactory
	// baseURL overrides the Anthropic API base URL in tests; empty means the
	// client default.
	baseURL   string
	pageLimit int
}

// NewAnthropicUsageAnalyticsPoller ingests the user_usage_report as the
// anthropic_analytics_usage schedule.
func NewAnthropicUsageAnalyticsPoller(
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, schedule string, page int),
) *AnthropicAnalyticsPoller {
	return newAnthropicAnalyticsPoller(store, guardianPolicy, telemetryLogger, heartbeat, ScheduleAnthropicAnalyticsUsage, NewAnthropicAnalyticsUsageSource)
}

// NewAnthropicCostAnalyticsPoller ingests the user_cost_report as the
// anthropic_analytics_cost schedule.
func NewAnthropicCostAnalyticsPoller(
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, schedule string, page int),
) *AnthropicAnalyticsPoller {
	return newAnthropicAnalyticsPoller(store, guardianPolicy, telemetryLogger, heartbeat, ScheduleAnthropicAnalyticsCost, NewAnthropicAnalyticsCostSource)
}

func newAnthropicAnalyticsPoller(
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, schedule string, page int),
	schedule string,
	newSource anthropicAnalyticsSourceFactory,
) *AnthropicAnalyticsPoller {
	if heartbeat == nil {
		panic("anthropic analytics poller requires heartbeat")
	}
	return &AnthropicAnalyticsPoller{
		store:           store,
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
		schedule:        schedule,
		newSource:       newSource,
		baseURL:         "",
		pageLimit:       0,
	}
}

func (p *AnthropicAnalyticsPoller) Sync(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderAnthropicCompliance {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for anthropic analytics: %s", cfg.Provider)
	}

	source, err := p.newSource(p.guardianPolicy, cfg, p.telemetryLogger.LogBulk, p.baseURL, p.pageLimit)
	if err != nil {
		return fmt.Errorf("build %s source: %w", p.schedule, err)
	}

	runner := &timewindowpoller.Poller[[]telemetry.LogParams]{
		Store:    p.store,
		Schedule: p.schedule,
		State: timewindowpoller.SyncState{
			SyncID:      cfg.SyncID,
			WatermarkAt: cfg.PollWatermarkAt,
			Checkpoint:  cfg.PollCheckpoint,
		},
		Source:  source,
		EndTime: endTime,
		Heartbeat: func(ctx context.Context, page int) {
			p.heartbeat(ctx, p.schedule, page)
		},
		InitialLookback: AnthropicAnalyticsInitialLookback,
		MaxWindow:       anthropicAnalyticsMaxWindow,
		Granularity:     time.Minute,
		ResumeOffset:    0,
	}
	if err := runner.Do(ctx); err != nil {
		return fmt.Errorf("sync %s: %w", p.schedule, err)
	}
	return nil
}

func NewAnthropicAnalyticsUsageSource(
	guardianPolicy *guardian.Policy,
	cfg Config,
	processPage func(ctx context.Context, payload []telemetry.LogParams) error,
	baseURL string,
	pageLimit int,
) (timewindowpoller.Source[[]telemetry.LogParams], error) {
	cfg, client, err := newAnthropicAnalyticsClient(guardianPolicy, ScheduleAnthropicAnalyticsUsage, cfg, baseURL)
	if err != nil {
		return nil, err
	}
	if processPage == nil {
		return nil, fmt.Errorf("process page is required")
	}
	return &anthropicUsageReportSource{
		client:      client,
		cfg:         cfg,
		pageLimit:   analyticsPageLimit(pageLimit),
		processPage: processPage,
	}, nil
}

func NewAnthropicAnalyticsCostSource(
	guardianPolicy *guardian.Policy,
	cfg Config,
	processPage func(ctx context.Context, payload []telemetry.LogParams) error,
	baseURL string,
	pageLimit int,
) (timewindowpoller.Source[[]telemetry.LogParams], error) {
	cfg, client, err := newAnthropicAnalyticsClient(guardianPolicy, ScheduleAnthropicAnalyticsCost, cfg, baseURL)
	if err != nil {
		return nil, err
	}
	if processPage == nil {
		return nil, fmt.Errorf("process page is required")
	}
	return &anthropicCostReportSource{
		client:      client,
		cfg:         cfg,
		pageLimit:   analyticsPageLimit(pageLimit),
		processPage: processPage,
	}, nil
}

func newAnthropicAnalyticsClient(guardianPolicy *guardian.Policy, schedule string, cfg Config, baseURL string) (Config, *anthropicapi.Client, error) {
	if guardianPolicy == nil {
		return cfg, nil, fmt.Errorf("guardian policy is required")
	}
	if cfg.Provider == "" {
		cfg.Provider = ProviderAnthropicCompliance
	}
	if cfg.Provider != ProviderAnthropicCompliance {
		return cfg, nil, fmt.Errorf("schedule %q requires provider %q, got %q", schedule, ProviderAnthropicCompliance, cfg.Provider)
	}
	return cfg, anthropicapi.New(guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey), anthropicapi.WithBaseURL(baseURL)), nil
}

func analyticsPageLimit(limit int) int {
	if limit > 0 {
		return limit
	}
	return anthropicAnalyticsPageLimit
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

func analyticsWindowParams(start, end time.Time, page string, limit int) anthropicapi.UserAnalyticsReportParams {
	return anthropicapi.UserAnalyticsReportParams{
		StartingAt:  start,
		EndingAt:    end,
		BucketWidth: anthropicAnalyticsBucketWidth,
		Products:    []string{anthropicAnalyticsProductChat},
		GroupBy:     []string{"model"},
		Limit:       limit,
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
	client      *anthropicapi.Client
	cfg         Config
	pageLimit   int
	processPage func(ctx context.Context, payload []telemetry.LogParams) error
}

func (src *anthropicUsageReportSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	page, err := src.client.GetUserUsageReport(ctx, analyticsProbeParams(endTime))
	if err != nil {
		return time.Time{}, err //nolint:wrapcheck // Preserve HTTPError for the access-denied classification upstream.
	}
	// An empty data_refreshed_at means Anthropic has not finalized any
	// analytics data for this org yet. Report a zero upper bound so the
	// poller no-ops the run instead of failing on an unparseable watermark.
	if page.DataRefreshedAt == "" {
		return time.Time{}, nil
	}
	refreshedAt, err := time.Parse(time.RFC3339, page.DataRefreshedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse usage report data_refreshed_at: %w", err)
	}
	return refreshedAt.UTC(), nil
}

func (src *anthropicUsageReportSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (timewindowpoller.Page[[]telemetry.LogParams], error) {
	res, err := src.client.GetUserUsageReport(ctx, analyticsWindowParams(start, end, pageToken, src.pageLimit))
	if err != nil {
		return timewindowpoller.Page[[]telemetry.LogParams]{Payload: nil, NextPage: "", HasMore: false}, err //nolint:wrapcheck // Preserve HTTPError for access-denied and rate-limit handling upstream.
	}
	rows, err := buildClaudeChatUsageRows(src.cfg, res.Data)
	if err != nil {
		return timewindowpoller.Page[[]telemetry.LogParams]{Payload: nil, NextPage: "", HasMore: false}, err
	}
	return timewindowpoller.Page[[]telemetry.LogParams]{
		Payload:  rows,
		NextPage: res.NextPage,
		HasMore:  res.HasMore,
	}, nil
}

func (src *anthropicUsageReportSource) ProcessPage(ctx context.Context, payload []telemetry.LogParams) error {
	return src.processPage(ctx, payload)
}

func (src *anthropicUsageReportSource) RetryAfter(err error) (time.Duration, bool) {
	var httpErr *anthropicapi.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	return 0, true
}

// anthropicCostReportSource pulls the user_cost_report for the
// anthropic_analytics_cost schedule, mirroring the usage source.
type anthropicCostReportSource struct {
	client      *anthropicapi.Client
	cfg         Config
	pageLimit   int
	processPage func(ctx context.Context, payload []telemetry.LogParams) error
}

func (src *anthropicCostReportSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
	page, err := src.client.GetUserCostReport(ctx, analyticsProbeParams(endTime))
	if err != nil {
		return time.Time{}, err //nolint:wrapcheck // Preserve HTTPError for the access-denied classification upstream.
	}
	// An empty data_refreshed_at means Anthropic has not finalized any
	// analytics data for this org yet. Report a zero upper bound so the
	// poller no-ops the run instead of failing on an unparseable watermark.
	if page.DataRefreshedAt == "" {
		return time.Time{}, nil
	}
	refreshedAt, err := time.Parse(time.RFC3339, page.DataRefreshedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse cost report data_refreshed_at: %w", err)
	}
	return refreshedAt.UTC(), nil
}

func (src *anthropicCostReportSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (timewindowpoller.Page[[]telemetry.LogParams], error) {
	res, err := src.client.GetUserCostReport(ctx, analyticsWindowParams(start, end, pageToken, src.pageLimit))
	if err != nil {
		return timewindowpoller.Page[[]telemetry.LogParams]{Payload: nil, NextPage: "", HasMore: false}, err //nolint:wrapcheck // Preserve HTTPError for access-denied and rate-limit handling upstream.
	}
	rows, err := buildClaudeChatCostRows(src.cfg, res.Data)
	if err != nil {
		return timewindowpoller.Page[[]telemetry.LogParams]{Payload: nil, NextPage: "", HasMore: false}, err
	}
	return timewindowpoller.Page[[]telemetry.LogParams]{
		Payload:  rows,
		NextPage: res.NextPage,
		HasMore:  res.HasMore,
	}, nil
}

func (src *anthropicCostReportSource) ProcessPage(ctx context.Context, payload []telemetry.LogParams) error {
	return src.processPage(ctx, payload)
}

func (src *anthropicCostReportSource) RetryAfter(err error) (time.Duration, bool) {
	var httpErr *anthropicapi.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	return 0, true
}

// buildClaudeChatUsageRows converts usage-report rows into
// claude_chat:usage:metrics telemetry rows. Usage and cost are ingested
// independently — no join — so spend in a bucket without token usage (e.g.
// web-search charges) is never dropped, and summaries include both row kinds.
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
// deterministic — re-fetching the same window yields byte-identical rows —
// so the aggregation key and values form a stable row identity. The leading
// kind literal keeps a usage row and a cost row for the same (bucket, user,
// model) from colliding.

func generateClaudeChatUsageRowHash(row anthropicapi.UserUsageRow) string {
	return eventKey{
		"usage",
		row.StartingAt,
		row.Actor.UserID,
		row.Model,
		row.Product,
		row.UncachedInputTokens,
		row.OutputTokens,
		row.CacheReadInputTokens,
		row.CacheCreation.Ephemeral1hInputTokens,
		row.CacheCreation.Ephemeral5mInputTokens,
		row.Requests,
	}.hash()
}

func generateClaudeChatCostRowHash(row anthropicapi.UserCostRow) string {
	return eventKey{
		"cost",
		row.StartingAt,
		row.Actor.UserID,
		row.Model,
		row.Product,
		row.Amount,
		row.Currency,
		row.Requests,
	}.hash()
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
