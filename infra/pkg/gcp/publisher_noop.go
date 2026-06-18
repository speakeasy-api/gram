package gcp

import "context"

type NoopPublisher[M any] struct{}

func NewNoopPublisher[M any]() *NoopPublisher[M] {
	return &NoopPublisher[M]{}
}

func (n *NoopPublisher[M]) Publish(ctx context.Context, msg M) PublishResult {
	return NewSuccessPublishResult()
}

func (n *NoopPublisher[M]) Stop(context.Context) error {
	return nil
}
