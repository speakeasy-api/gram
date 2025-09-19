package o11y

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	charmlog "github.com/charmbracelet/log"
	"go.opentelemetry.io/otel/trace"
)

type LogHandlerOptions struct {
	// RawLevel is the log level as a string, e.g., "info", "debug", etc.
	RawLevel string
	// Pretty indicates whether to use pretty logging.
	Pretty      bool
	DataDogAttr bool
}

func NewLogHandler(opts *LogHandlerOptions) slog.Handler {
	rl := opts.RawLevel
	if rl == "" {
		rl = "error"
	}

	if opts.Pretty {
		return &ContextHandler{
			DataDogAttr: opts.DataDogAttr,
			Handler: charmlog.NewWithOptions(os.Stderr, charmlog.Options{
				ReportCaller: true,
				Level:        Levels[rl].Charm,
			}),
		}
	} else {
		return &ContextHandler{
			DataDogAttr: opts.DataDogAttr,
			Handler: slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				AddSource:   true,
				Level:       Levels[rl].Slog,
				ReplaceAttr: nil,
			}),
		}
	}
}

type ContextHandler struct {
	Handler     slog.Handler
	DataDogAttr bool
}

var _ slog.Handler = (*ContextHandler)(nil)

func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{
		Handler:     h.Handler.WithAttrs(attrs),
		DataDogAttr: h.DataDogAttr,
	}
}

func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{
		Handler:     h.Handler.WithGroup(name),
		DataDogAttr: h.DataDogAttr,
	}
}

func (h *ContextHandler) Handle(ctx context.Context, record slog.Record) error {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		id := spanCtx.TraceID().String()
		record.Add(slog.String("trace.id", id))
		if h.DataDogAttr {
			record.Add(slog.String("dd.trace_id", id))
		}
	}
	if spanCtx.HasSpanID() {
		id := spanCtx.SpanID().String()
		record.Add(slog.String("span.id", id))
		if h.DataDogAttr {
			record.Add(slog.String("dd.span_id", id))
		}
	}

	err := h.Handler.Handle(ctx, record)
	if err != nil {
		return fmt.Errorf("contexthandler: handle slog record: %w", err)
	}

	return nil
}

func LogDefer(ctx context.Context, logger *slog.Logger, cb func() error) error {
	err := cb()
	if err == nil {
		return nil
	}

	logger.ErrorContext(ctx, "error", slog.String("error", err.Error()))

	return err
}

func NoLogDefer(cb func() error) {
	_ = cb()
}
