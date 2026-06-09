package ping

import (
	"context"
	"log/slog"

	pingv1 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

type Handler struct {
	logger *slog.Logger
}

func NewHandler(logger *slog.Logger) *Handler {
	return &Handler{
		logger: logger,
	}
}

func (h *Handler) Handle(ctx context.Context, m *pingv1.Message, _ gcp.MessageMetadata) error {
	logger := h.logger

	logger.DebugContext(ctx, "ping subscriber received message", attr.SlogValueAny(map[string]any{
		"id":      m.GetId(),
		"type":    m.GetType(),
		"payload": string(m.GetPayload()),
	}))

	return nil
}
