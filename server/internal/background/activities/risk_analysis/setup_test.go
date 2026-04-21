package risk_analysis_test

import (
	"log/slog"
	"os"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace/noop"
)

var (
	testLogger         = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	testTracerProvider = noop.NewTracerProvider()
	testMeterProvider  = metricnoop.NewMeterProvider()
)
