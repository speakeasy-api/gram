package risk

import (
	"context"
	"log/slog"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

type PresidioRequestHandler struct {
	logger *slog.Logger
}

func NewPresidioRequestHandler(logger *slog.Logger) *PresidioRequestHandler {
	return &PresidioRequestHandler{
		logger: logger,
	}
}

func (h *PresidioRequestHandler) Handle(ctx context.Context, msg *riskv1.PresidioRequest, _ gcp.MessageMetadata) error {
	h.logger.InfoContext(ctx, "discarding presidio scan request", attr.SlogValueString(msg.GetId()))

	return nil
}
