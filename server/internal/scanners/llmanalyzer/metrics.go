package llmanalyzer

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	meterRequests      = "risk.llm.requests"
	meterDuration      = "risk.llm.duration"
	meterUsage         = "risk.llm.tokens"
	meterRetries       = "risk.llm.retries"
	meterParseFailures = "risk.llm.parse_failures"

	// TokenKindInput and TokenKindOutput are the gram.risk.llm.token_kind
	// values on risk.llm.tokens.
	TokenKindInput  = "input"
	TokenKindOutput = "output"
)

const (
	// OutcomeRateLimited is the request outcome when the upstream kept
	// answering 429 until the retry budget ran out.
	OutcomeRateLimited o11y.Outcome = "rate_limited"
)

// Scan modes reported on every risk model call: how the scan was invoked,
// distinct from the enforcement dispatcher's lane (scanner + policy).
const (
	// ScanModeSync marks realtime enforcement calls.
	ScanModeSync = "sync"
	// ScanModeAsync marks batch scan calls.
	ScanModeAsync = "async"
)

// CallInfo carries the metric and span dimensions of one Complete call. The
// client does not know which organization or lane it serves; the analyzer
// passes them in.
type CallInfo struct {
	// OrgID is the organization the evaluated message belongs to.
	OrgID string

	// OrgSlug is the organization's slug, for dashboards that slice by name.
	OrgSlug string

	// ScanMode is ScanModeSync for realtime enforcement and ScanModeAsync
	// for batch scans.
	ScanMode string
}

type metrics struct {
	requests      metric.Int64Counter
	duration      metric.Float64Histogram
	tokens        metric.Int64Counter
	retries       metric.Int64Counter
	parseFailures metric.Int64Counter
}

func newMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *metrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/scanners/llmanalyzer")

	requests, err := meter.Int64Counter(
		meterRequests,
		metric.WithDescription("Completion calls made to the fine-tuned risk model, retries collapsed into one"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRequests), attr.SlogError(err))
	}

	duration, err := meter.Float64Histogram(
		meterDuration,
		metric.WithDescription("End-to-end duration of a risk model completion call in seconds, retries included"),
		metric.WithUnit("s"),
		// Buckets hug the 15s call timeout: coarse below 1s, finer as latency
		// approaches the budget, and a 20s boundary so timed-out calls that
		// overshoot still land in a bounded bucket.
		metric.WithExplicitBucketBoundaries(0.25, 0.5, 1, 2, 3, 5, 8, 10, 12, 15, 20),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterDuration), attr.SlogError(err))
	}

	tokens, err := meter.Int64Counter(
		meterUsage,
		metric.WithDescription("Prompt and completion tokens reported by the risk model, split by gram.risk.llm.token_kind"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterUsage), attr.SlogError(err))
	}

	retries, err := meter.Int64Counter(
		meterRetries,
		metric.WithDescription("HTTP attempts beyond the first made while completing a risk model call"),
		metric.WithUnit("{retry}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterRetries), attr.SlogError(err))
	}

	parseFailures, err := meter.Int64Counter(
		meterParseFailures,
		metric.WithDescription("Risk model completions that could not be parsed as a verdict"),
		metric.WithUnit("{failure}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterParseFailures), attr.SlogError(err))
	}

	return &metrics{
		requests:      requests,
		duration:      duration,
		tokens:        tokens,
		retries:       retries,
		parseFailures: parseFailures,
	}
}

// RecordRequest records the outcome and end-to-end latency of one Complete
// call.
func (m *metrics) RecordRequest(ctx context.Context, info CallInfo, model string, outcome o11y.Outcome, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.OrganizationID(info.OrgID),
		attr.OrganizationSlug(info.OrgSlug),
		attr.RiskScanMode(info.ScanMode),
		attr.RiskLLMModel(model),
		attr.Outcome(outcome),
	)
	if m.requests != nil {
		m.requests.Add(ctx, 1, attrs)
	}
	if m.duration != nil {
		m.duration.Record(ctx, duration.Seconds(), attrs)
	}
}

// RecordTokens records the prompt and completion token counts of a successful
// call.
func (m *metrics) RecordTokens(ctx context.Context, info CallInfo, model string, promptTokens, completionTokens int) {
	if m.tokens == nil {
		return
	}
	m.tokens.Add(ctx, int64(promptTokens), metric.WithAttributes(
		attr.OrganizationID(info.OrgID),
		attr.OrganizationSlug(info.OrgSlug),
		attr.RiskScanMode(info.ScanMode),
		attr.RiskLLMModel(model),
		attr.RiskLLMTokenKind(TokenKindInput),
	))
	m.tokens.Add(ctx, int64(completionTokens), metric.WithAttributes(
		attr.OrganizationID(info.OrgID),
		attr.OrganizationSlug(info.OrgSlug),
		attr.RiskScanMode(info.ScanMode),
		attr.RiskLLMModel(model),
		attr.RiskLLMTokenKind(TokenKindOutput),
	))
}

// RecordRetries records how many attempts beyond the first a call made. Zero
// retries record nothing.
func (m *metrics) RecordRetries(ctx context.Context, info CallInfo, model string, retries int) {
	if m.retries == nil || retries <= 0 {
		return
	}
	m.retries.Add(ctx, int64(retries), metric.WithAttributes(
		attr.OrganizationID(info.OrgID),
		attr.OrganizationSlug(info.OrgSlug),
		attr.RiskScanMode(info.ScanMode),
		attr.RiskLLMModel(model),
	))
}

// RecordParseFailure records a completion that ParseVerdict rejected. The
// call itself was already recorded as a success on risk.llm.requests: that
// counter tracks transport outcomes and this one tracks verdict quality.
func (m *metrics) RecordParseFailure(ctx context.Context, info CallInfo, model string) {
	if m.parseFailures == nil {
		return
	}
	m.parseFailures.Add(ctx, 1, metric.WithAttributes(
		attr.OrganizationID(info.OrgID),
		attr.OrganizationSlug(info.OrgSlug),
		attr.RiskScanMode(info.ScanMode),
		attr.RiskLLMModel(model),
	))
}
