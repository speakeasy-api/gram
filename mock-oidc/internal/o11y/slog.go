package o11y

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/plog"
)

type LevelMapping struct {
	Slog slog.Level
	Plog int
}

var LogLevels = map[string]LevelMapping{
	"debug": {Slog: slog.LevelDebug, Plog: plog.LevelDebug},
	"info":  {Slog: slog.LevelInfo, Plog: plog.LevelInfo},
	"warn":  {Slog: slog.LevelWarn, Plog: plog.LevelWarn},
	"error": {Slog: slog.LevelError, Plog: plog.LevelError},
}

type LogHandlerOptions struct {
	RawLevel string
	Pretty   bool
}

func NewLogHandler(opts *LogHandlerOptions) slog.Handler {
	rl := opts.RawLevel
	if rl == "" {
		rl = "info"
	}

	if opts.Pretty {
		return &ContextHandler{
			Handler: plog.NewHandler(
				plog.WithAddSource(true),
				plog.WithHideTimestamp(true),
				plog.WithLevel(LogLevels[rl].Plog),
			),
		}
	}

	return &ContextHandler{
		Handler: slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			AddSource: true,
			Level:     LogLevels[rl].Slog,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if len(groups) == 0 && a.Key == slog.TimeKey {
					a.Key = "timestamp"
				}
				return a
			},
		}),
	}
}

type ContextHandler struct {
	Handler slog.Handler
}

var _ slog.Handler = (*ContextHandler)(nil)

func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{Handler: h.Handler.WithGroup(name)}
}

func (h *ContextHandler) Handle(ctx context.Context, record slog.Record) error {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		record.Add(slog.String("trace.id", spanCtx.TraceID().String()))
	}
	if spanCtx.HasSpanID() {
		record.Add(slog.String("span.id", spanCtx.SpanID().String()))
	}

	if err := h.Handler.Handle(ctx, record); err != nil {
		return fmt.Errorf("contexthandler: handle slog record: %w", err)
	}
	return nil
}

func ErrAttr(err error) slog.Attr {
	return slog.String("error.message", err.Error())
}
