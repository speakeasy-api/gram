package gitleaks

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

type enforceHandlerMetrics struct {
	staleDropped     metric.Int64Counter
	replyWriteErrors metric.Int64Counter
}

func newEnforceHandlerMetrics(meterProvider metric.MeterProvider) (enforceHandlerMetrics, error) {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/scanners/gitleaks")
	staleDropped, err := meter.Int64Counter(
		"risk.enforcement.gitleaks.stale_dropped",
		metric.WithDescription("Gitleaks enforcement requests acknowledged without scanning because they were stale"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return enforceHandlerMetrics{}, fmt.Errorf("create risk.enforcement.gitleaks.stale_dropped metric: %w", err)
	}
	replyWriteErrors, err := meter.Int64Counter(
		"risk.enforcement.gitleaks.reply_write_errors",
		metric.WithDescription("Gitleaks enforcement reply writes acknowledged after Redis failure"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return enforceHandlerMetrics{}, fmt.Errorf("create risk.enforcement.gitleaks.reply_write_errors metric: %w", err)
	}
	return enforceHandlerMetrics{staleDropped: staleDropped, replyWriteErrors: replyWriteErrors}, nil
}
