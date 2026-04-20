package risk_analysis_test

import (
	"log/slog"
	"os"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
