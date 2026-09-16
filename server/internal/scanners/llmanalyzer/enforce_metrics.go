package llmanalyzer

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	meterEnforceRequests         = "risk.enforcement.llm.requests"
	meterEnforceStaleDropped     = "risk.enforcement.llm.stale_dropped"
	meterEnforceReplyWriteErrors = "risk.enforcement.llm.reply_write_errors"
)

// Outcomes recorded on risk.enforcement.llm.requests. They mirror the status
// written on the EnforcementReply so the dispatcher's view and the consumer's
// view of the lane can be reconciled.
const (
	EnforceOutcomeOK         o11y.Outcome = "ok"
	EnforceOutcomeError      o11y.Outcome = "error"
	EnforceOutcomeDeadLetter o11y.Outcome = "dead_letter"
)

type enforceHandlerMetrics struct {
	requests         metric.Int64Counter
	staleDropped     metric.Int64Counter
	replyWriteErrors metric.Int64Counter
}

func newEnforceHandlerMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) enforceHandlerMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter(tracerName)

	requests, err := meter.Int64Counter(
		meterEnforceRequests,
		metric.WithDescription("LLM enforcement requests answered, by reply status"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterEnforceRequests), attr.SlogError(err))
	}

	staleDropped, err := meter.Int64Counter(
		meterEnforceStaleDropped,
		metric.WithDescription("LLM enforcement requests acknowledged without analysis because their timestamp fell outside the freshness window (stale or far-future)"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterEnforceStaleDropped), attr.SlogError(err))
	}

	replyWriteErrors, err := meter.Int64Counter(
		meterEnforceReplyWriteErrors,
		metric.WithDescription("LLM enforcement reply writes acknowledged after Redis failure"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName(meterEnforceReplyWriteErrors), attr.SlogError(err))
	}

	return enforceHandlerMetrics{
		requests:         requests,
		staleDropped:     staleDropped,
		replyWriteErrors: replyWriteErrors,
	}
}

func (m enforceHandlerMetrics) recordRequest(ctx context.Context, outcome o11y.Outcome) {
	if m.requests == nil {
		return
	}
	m.requests.Add(ctx, 1, metric.WithAttributes(attr.RiskLane(LaneSync), attr.Outcome(outcome)))
}

func (m enforceHandlerMetrics) recordStaleDropped(ctx context.Context) {
	if m.staleDropped == nil {
		return
	}
	m.staleDropped.Add(ctx, 1, metric.WithAttributes(attr.RiskLane(LaneSync)))
}

func (m enforceHandlerMetrics) recordReplyWriteError(ctx context.Context) {
	if m.replyWriteErrors == nil {
		return
	}
	m.replyWriteErrors.Add(ctx, 1, metric.WithAttributes(attr.RiskLane(LaneSync)))
}
