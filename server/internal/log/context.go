package log

import (
	"context"
	"log/slog"
)

type loggerKey struct{}

func With(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

func From(ctx context.Context) *slog.Logger {
	return ctx.Value(loggerKey{}).(*slog.Logger)
}
