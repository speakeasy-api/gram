package subscribers

import (
	"context"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

type NoopHandler[T any] struct{}

func (h *NoopHandler[T]) Handle(context.Context, T, gcp.MessageMetadata) error {
	return nil
}
