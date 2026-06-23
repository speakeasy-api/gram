package ping

import (
	"context"
	"log/slog"

	pingv2 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

type Handler struct {
	logger   *slog.Logger
	logLevel slog.Level
}

func NewHandler(logger *slog.Logger, level slog.Level) *Handler {
	return &Handler{
		logger:   logger,
		logLevel: level,
	}
}

func (h *Handler) Handle(ctx context.Context, m *pingv2.Message, _ gcp.MessageMetadata) error {
	logger := h.logger

	logger.LogAttrs(ctx, h.logLevel, "ping subscriber received message", attr.SlogValueAny(map[string]any{
		"id":         m.GetId(),
		"type":       m.GetType(),
		"created_at": m.GetCreatedAt(),
		"payload":    string(m.GetPayload()),
	}))

	return nil
}
