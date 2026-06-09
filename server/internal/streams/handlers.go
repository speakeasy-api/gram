package streams

import (
	"context"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

type Handler[M any] interface {
	Handle(context.Context, M, gcp.MessageMetadata) error
}

type HandlerFunc[M any] func(context.Context, M, gcp.MessageMetadata) error

func (f HandlerFunc[M]) Handle(ctx context.Context, msg M, meta gcp.MessageMetadata) error {
	return f(ctx, msg, meta)
}
