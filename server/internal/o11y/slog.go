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
)

var temporalKeys = map[string]string{
	"Namespace":    "namespace",
	"TaskQueue":    "task_queue",
	"WorkerID":     "worker_id",
	"ActivityID":   "activity_id",
	"ActivityType": "activity_type",
	"Attempt":      "attempt",
	"WorkflowType": "workflow_type",
	"WorkflowID":   "workflow_id",
	"RunID":        "run_id",
}

func NewLogHandler(rawLevel string, pretty bool) slog.Handler {
	rl := rawLevel
	if rl == "" {
		rl = "error"
	}

	if pretty {
		return &ContextHandler{
			Handler: charmlog.NewWithOptions(os.Stderr, charmlog.Options{
				ReportCaller: true,
				Level:        LogLevels[rl].Charm,
			}),
		}
	} else {
		return &ContextHandler{
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

					a.Key = replace

					return a
				},
			}),
		}
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
		record.Add("trace_id", spanCtx.TraceID().String())
	}
	if spanCtx.HasSpanID() {
		record.Add("span_id", spanCtx.SpanID().String())
	}

	if service, ok := ctx.Value(goa.ServiceKey).(string); ok {
		record.Add("goa_service", service)
	}
	if method, ok := ctx.Value(goa.MethodKey).(string); ok {
		record.Add("goa_method", method)
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
