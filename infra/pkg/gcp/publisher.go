package gcp

import (
	"context"
	"fmt"

	"cloud.google.com/go/pubsub/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/protobuf/proto"
)

type PublisherBroker interface {
	PublisherForMessage(ctx context.Context, msg proto.Message) (*pubsub.Publisher, error)
}

// isNilMessage reports whether a proto message is unusable as input: either a
// nil interface or a typed-nil pointer (an invalid reflect message). Guarding
// on this at the boundary lets callers receive a typed error instead of a
// panic when ProtoReflect is dereferenced downstream.
func isNilMessage(m proto.Message) bool {
	return m == nil || !m.ProtoReflect().IsValid()
}

type PublishResult interface {
	Ready() <-chan struct{}
	Get(ctx context.Context) (serverID string, err error)
}

type errPublishResult struct {
	err error
}

func (e *errPublishResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (e *errPublishResult) Get(ctx context.Context) (serverID string, err error) {
	return "", e.err
}

type Publisher[M any] interface {
	Publish(ctx context.Context, msg M) PublishResult
}

type psPublisherOptions struct {
	propagation     propagation.TextMapPropagator
	publishSettings *pubsub.PublishSettings
}

func WithPropagator(prop propagation.TextMapPropagator) func(*psPublisherOptions) {
	return func(o *psPublisherOptions) {
		o.propagation = prop
	}
}

func WithPubSubPublishSettings(settings *pubsub.PublishSettings) func(*psPublisherOptions) {
	return func(o *psPublisherOptions) {
		if settings != nil {
			o.publishSettings = settings
		}
	}
}

type psPublisher[M proto.Message] struct {
	pub  *pubsub.Publisher
	prop propagation.TextMapPropagator
}

// PubSubPublisherForMessage returns a publisher for the topic declared by a
// protobuf message's (gcp.pubsub.v1.topic) option. It errors if msg does not
// declare a topic.
func PubSubPublisherForMessage[M proto.Message](ctx context.Context, broker PublisherBroker, msg M, opts ...func(*psPublisherOptions)) (Publisher[M], error) {
	if isNilMessage(msg) {
		return nil, fmt.Errorf("message must not be nil")
	}

	publisher, err := broker.PublisherForMessage(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("get publisher for message: %w", err)
	}

	var o psPublisherOptions
	o.propagation = otel.GetTextMapPropagator()
	for _, opt := range opts {
		opt(&o)
	}
	if o.publishSettings != nil {
		publisher.PublishSettings = *o.publishSettings
	}

	return &psPublisher[M]{pub: publisher, prop: o.propagation}, nil
}

// messageAttributes builds the attribute set carried with an outgoing message:
// the content-type and schema markers the subscriber uses to decode the
// payload, plus any trace context propagated from ctx so the subscriber can
// continue the producer's trace. The propagator is passed in so the behaviour
// is testable without mutating global state; when ctx carries no active span
// injection is a no-op and no propagation attributes are added.
func messageAttributes(ctx context.Context, prop propagation.TextMapPropagator, msg proto.Message) map[string]string {
	attributes := map[string]string{
		"content-type": "application/x-protobuf",
		"schema":       string(proto.MessageName(msg)),
	}
	if prop != nil {
		prop.Inject(ctx, propagation.MapCarrier(attributes))
	}
	return attributes
}

func (p *psPublisher[M]) Publish(ctx context.Context, msg M) PublishResult {
	bs, err := proto.Marshal(msg)
	if err != nil {
		return &errPublishResult{err: fmt.Errorf("marshal proto: %w", err)}
	}

	res := p.pub.Publish(ctx, &pubsub.Message{
		Data:       bs,
		Attributes: messageAttributes(ctx, p.prop, msg),
	})

	return res
}
