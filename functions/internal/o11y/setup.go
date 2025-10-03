package o11y

import (
	"context"
	"log/slog"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/functions/internal/attr"
)

// SetupOTelSDK bootstraps the OpenTelemetry pipeline.
func SetupOTelSDK(ctx context.Context, logger *slog.Logger) {
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	otellogger := logr.FromSlogHandler(logger.Handler())
	otel.SetMeterProvider(metricnoop.NewMeterProvider())
	otel.SetTracerProvider(tracenoop.NewTracerProvider())
	otel.SetTextMapPropagator(prop)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.ErrorContext(ctx, "otel error", attr.SlogError(err))
	}))
	otel.SetLogger(otellogger)
}
