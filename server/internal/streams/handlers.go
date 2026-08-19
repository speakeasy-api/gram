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

// BatchHandler processes a batch of messages at once. The metadata slice is
// parallel to the message slice (one entry per message, same order). Returning
// nil acks the whole batch; returning an error nacks the whole batch.
type BatchHandler[M any] interface {
	HandleBatch(context.Context, []M, []gcp.MessageMetadata) error
}

type BatchHandlerFunc[M any] func(context.Context, []M, []gcp.MessageMetadata) error

func (f BatchHandlerFunc[M]) HandleBatch(ctx context.Context, msgs []M, metas []gcp.MessageMetadata) error {
	return f(ctx, msgs, metas)
}

// BatchMessage is a decoded message whose Fail method stages a nack until its
// batch handler returns.
type BatchMessage[M any] = gcp.BatchMessage[M]

// BatchResultHandler processes a batch while allowing individual messages to
// stage failures. Returning nil nacks failed messages and acks the rest.
// Returning an error nacks the whole batch.
type BatchResultHandler[M any] interface {
	HandleBatchWithResult(context.Context, []BatchMessage[M]) error
}

type BatchResultHandlerFunc[M any] func(context.Context, []BatchMessage[M]) error

func (f BatchResultHandlerFunc[M]) HandleBatchWithResult(ctx context.Context, msgs []BatchMessage[M]) error {
	return f(ctx, msgs)
}
