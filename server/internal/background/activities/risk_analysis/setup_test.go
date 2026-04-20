package risk_analysis_test

import (
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace/noop"
)

var (
	testLogger         = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	testTracerProvider = noop.NewTracerProvider()
)
