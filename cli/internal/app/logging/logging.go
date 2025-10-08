package logging

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

func PushLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

func PullLogger(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(loggerKey{}).(*slog.Logger)
	if !ok || logger == nil {
		panic("PullLogger: logger not found in context")
	}

	return logger
}
