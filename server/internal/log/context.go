package log

import (
	"context"
	"log/slog"

	goa "goa.design/goa/v3/pkg"
)

type ContextHandler struct {
	Handler slog.Handler
}

var _ slog.Handler = &ContextHandler{}

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
	if service, ok := ctx.Value(goa.ServiceKey).(string); ok {
		record.Add("goa.service", service)
	}
	if method, ok := ctx.Value(goa.MethodKey).(string); ok {
		record.Add("goa.method", method)
	}

	return h.Handler.Handle(ctx, record)
}
