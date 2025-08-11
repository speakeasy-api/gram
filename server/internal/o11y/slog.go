package o11y

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	charmlog "github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5/tracelog"
	"go.opentelemetry.io/otel/trace"
	goa "goa.design/goa/v3/pkg"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

var temporalKeys = map[string]attr.Key{
	"Namespace":    attr.TemporalNamespaceNameKey,
	"TaskQueue":    attr.TemporalTaskQueueNameKey,
	"WorkerID":     attr.TemporalWorkerIDKey,
	"ActivityID":   attr.TemporalActivityIDKey,
	"ActivityType": attr.TemporalActivityTypeKey,
	"Attempt":      attr.TemporalAttemptKey,
	"WorkflowType": attr.TemporalWorkflowTypeKey,
	"WorkflowID":   attr.TemporalWorkflowIDKey,
	"RunID":        attr.TemporalRunIDKey,
}

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
			Handler: charmlog.NewWithOptions(os.Stderr, charmlog.Options{
				ReportCaller: true,
				Level:        LogLevels[rl].Charm,
			}),
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

					replace, ok := temporalKeys[a.Key]
					if !ok {
						return a
					}

					a.Key = string(replace)

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
		if h.DataDogAttr {
			record.Add(attr.SlogDataDogTraceID(id))
		} else {
			record.Add(attr.SlogTraceID(id))
		}
	}
	if spanCtx.HasSpanID() {
		id := spanCtx.SpanID().String()
		if h.DataDogAttr {
			record.Add(attr.SlogDataDogSpanID(id))
		} else {
			record.Add(attr.SlogSpanID(id))
		}
	}

	if service, ok := ctx.Value(goa.ServiceKey).(string); ok {
		record.Add(attr.SlogGoaService(service))
	}
	if method, ok := ctx.Value(goa.MethodKey).(string); ok {
		record.Add(attr.SlogGoaMethod(method))
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

type pgxLogger struct {
	logger *slog.Logger
}

func (l *pgxLogger) Log(ctx context.Context, level tracelog.LogLevel, msg string, data map[string]any) {
	if level == tracelog.LogLevelNone {
		return
	}

	lvl, ok := pgxLevels[level]
	if !ok {
		lvl = slog.LevelDebug
	}

	attr := make([]any, 0, len(data))
	for k, v := range data {
		attr = append(attr, slog.Any(k, v))
	}

	l.logger.Log(ctx, lvl, msg, attr...)
}

func NewPGXLogger(logger *slog.Logger, level tracelog.LogLevel) *tracelog.TraceLog {
	return &tracelog.TraceLog{
		Logger:   &pgxLogger{logger: logger},
		LogLevel: level,
		Config:   tracelog.DefaultTraceLogConfig(),
	}
}
