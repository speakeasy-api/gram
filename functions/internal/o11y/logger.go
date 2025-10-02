package o11y

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/speakeasy-api/gram/functions/internal/attr"
	"go.opentelemetry.io/otel/trace"
)

type LoggerOptions struct {
	Pretty      bool
	DataDogAttr bool
}

func NewLogger(w io.Writer, options LoggerOptions) *slog.Logger {
	var h slog.Handler
	if options.Pretty {
		h = slog.NewTextHandler(w, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	} else {
		h = slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}

	return slog.New(&ContextHandler{
		Handler:     h,
		DataDogAttr: options.DataDogAttr,
	})
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
		record.Add(attr.SlogTraceID(id))
		if h.DataDogAttr {
			record.Add(attr.SlogDataDogTraceID(id))
		}
	}
	if spanCtx.HasSpanID() {
		id := spanCtx.SpanID().String()
		record.Add(attr.SlogSpanID(id))
		if h.DataDogAttr {
			record.Add(attr.SlogDataDogSpanID(id))
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

	logger.ErrorContext(ctx, "error", attr.SlogError(err))

	return err
}

func NoLogDefer(cb func() error) {
	_ = cb()
}
