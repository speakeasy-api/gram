package o11y

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/infra/internal/attr"
	"github.com/speakeasy-api/gram/plog"
)

type LogHandlerOptions struct {
	// RawLevel is the log level as a string, e.g., "info", "debug", etc.
	RawLevel string
	// Pretty indicates whether to use pretty logging.
	Pretty bool
	// DataDogAttr indicates whether to add DataDog specific attributes to logs
	// instead of vendor-agnostic equivalents.
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
			Handler: plog.NewHandler(
				plog.WithAddSource(true),
				plog.WithHideTimestamp(true),
				plog.WithLevel(LogLevels[rl].Plog),
				plog.WithOmitKeys(
					string(attr.ServiceEnvKey),
					"service.*",
					"git.*",
				),
			),
		}
	} else {
		return &ContextHandler{
			DataDogAttr: opts.DataDogAttr,
			Handler: slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				AddSource: true,
				Level:     LogLevels[rl].Slog,
				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					if len(groups) != 0 {
						return a
					}

					if a.Key == slog.TimeKey {
						a.Key = "timestamp"
						return a
					}

					return a
				},
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
