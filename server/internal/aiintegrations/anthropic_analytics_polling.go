package aiintegrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

// anthropicAnalyticsSourceFactory builds the report-specific source for one
// sync, bound to that sync's API client and config.
type anthropicAnalyticsSourceFactory func(client *anthropicapi.Client, cfg Config) source[[]telemetry.LogParams]

// AnthropicAnalyticsPoller ingests one Admin Analytics report — usage or
// cost — from the Anthropic API into telemetry_logs, where the
// attribute_metrics_summaries MV picks it up for tokens-under-management
// billing. Each report is its own service instance driving its own sync
// schedule: construct one with NewAnthropicUsageAnalyticsPoller and one with
// NewAnthropicCostAnalyticsPoller. Usage is ingested per
// (user, minute, model) without any session linkage — the Compliance API
// import owns session content.
type AnthropicAnalyticsPoller struct {
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
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, scope string, page int),
) *AnthropicAnalyticsPoller {
	return newAnthropicAnalyticsPoller(store, guardianPolicy, telemetryLogger, heartbeat, ScheduleAnthropicAnalyticsUsage,
		func(client *anthropicapi.Client, cfg Config) source[[]telemetry.LogParams] {
			return &anthropicUsageReportSource{client: client, cfg: cfg}
		})
}

// NewAnthropicCostAnalyticsPoller ingests the user_cost_report (spend) as
// the anthropic_analytics_cost schedule.
func NewAnthropicCostAnalyticsPoller(
	store *Store,
	guardianPolicy *guardian.Policy,
	telemetryLogger *telemetry.Logger,
	heartbeat func(ctx context.Context, scope string, page int),
) *AnthropicAnalyticsPoller {
	return newAnthropicAnalyticsPoller(store, guardianPolicy, telemetryLogger, heartbeat, ScheduleAnthropicAnalyticsCost,
		func(client *anthropicapi.Client, cfg Config) source[[]telemetry.LogParams] {
			return &anthropicCostReportSource{client: client, cfg: cfg}
		})
}

func newAnthropicAnalyticsPoller(
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
		store:           store,
		guardianPolicy:  guardianPolicy,
		telemetryLogger: telemetryLogger,
		heartbeat:       heartbeat,
		schedule:        schedule,
		newSource:       newSource,
		baseURL:         "",
	}
}

// Sync ingests one Admin Analytics report schedule. Its caller runs this in a
// dedicated Temporal workflow, so errors are returned for independent retries
// and failure recording. Non-Enterprise organizations (or keys without the
// read:analytics scope) receive a classified access error.
func (p *AnthropicAnalyticsPoller) Sync(ctx context.Context, cfg Config, endTime time.Time) error {
	if cfg.Provider != ProviderAnthropicCompliance {
		return oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider for anthropic analytics: %s", cfg.Provider)
	}

	client := anthropicapi.New(p.guardianPolicy, anthropicapi.WithAPIKey(cfg.APIKey), anthropicapi.WithBaseURL(p.baseURL))

	runner := &poller[[]telemetry.LogParams]{
		store:    p.store,
		schedule: p.schedule,
		heartbeat: func(ctx context.Context, page int) {
			p.heartbeat(ctx, p.schedule, page)
		},
		processPage:     p.telemetryLogger.LogBulk,
		initialLookback: anthropicAnalyticsInitialLookback,
		maxWindow:       anthropicAnalyticsMaxWindow,
		granularity:     time.Minute,
	}
	if err := runner.sync(ctx, cfg, cfg.PollWatermarkAt, p.newSource(client, cfg), endTime); err != nil {
		return classifyAnthropicAnalyticsErr(err)
	}
	return nil
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
	client *anthropicapi.Client
	cfg    Config
}

func (src *anthropicUsageReportSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
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

func (src *anthropicUsageReportSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (page[[]telemetry.LogParams], error) {
	res, err := src.client.GetUserUsageReport(ctx, analyticsWindowParams(start, end, pageToken))
	if err != nil {
		return page[[]telemetry.LogParams]{}, err //nolint:wrapcheck // Preserve HTTPError for access-denied and rate-limit handling upstream.
	}
	rows, err := buildClaudeChatUsageRows(src.cfg, res.Data)
	if err != nil {
		return page[[]telemetry.LogParams]{}, err
	}
	return page[[]telemetry.LogParams]{
		Payload:  rows,
		NextPage: res.NextPage,
		HasMore:  res.HasMore,
	}, nil
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
	client *anthropicapi.Client
	cfg    Config
}

func (src *anthropicCostReportSource) UpperBound(ctx context.Context, endTime time.Time) (time.Time, error) {
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

func (src *anthropicCostReportSource) FetchPage(ctx context.Context, start, end time.Time, pageToken string) (page[[]telemetry.LogParams], error) {
	res, err := src.client.GetUserCostReport(ctx, analyticsWindowParams(start, end, pageToken))
	if err != nil {
		return page[[]telemetry.LogParams]{}, err //nolint:wrapcheck // Preserve HTTPError for access-denied and rate-limit handling upstream.
	}
	rows, err := buildClaudeChatCostRows(src.cfg, res.Data)
	if err != nil {
		return page[[]telemetry.LogParams]{}, err
	}
	return page[[]telemetry.LogParams]{
		Payload:  rows,
		NextPage: res.NextPage,
		HasMore:  res.HasMore,
	}, nil
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
