package gcp

import (
	"context"
	"fmt"
	"maps"

	"cloud.google.com/go/pubsub/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/protobuf/proto"
)

type PublisherBroker interface {
	PublisherForMessage(ctx context.Context, msg proto.Message) (*pubsub.Publisher, error)
}

type Publisher[M any] interface {
	Publish(ctx context.Context, msg M, opts ...PublishOption) PublishResult
	Stop(ctx context.Context) error
}

// PublishOptions configures one published message.
type PublishOptions struct {
	// Attributes are merged under the publisher's derived content-type and
	// schema attributes.
	Attributes map[string]string
}

// PublishOption configures one published message.
type PublishOption func(*PublishOptions)

// WithMessageAttributes adds attributes to one published message. Later
// options replace earlier values with the same key.
func WithMessageAttributes(attributes map[string]string) PublishOption {
	return func(opts *PublishOptions) {
		if opts.Attributes == nil {
			opts.Attributes = make(map[string]string, len(attributes))
		}
		maps.Copy(opts.Attributes, attributes)
	}
}

func resolvePublishOptions(options []PublishOption) PublishOptions {
	var opts PublishOptions
	for _, option := range options {
		option(&opts)
	}
	return opts
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

// NewErrPublishResult returns a PublishResult already settled with err, for
// failures that happen before a message reaches Pub/Sub. It is the counterpart
// of NewSuccessPublishResult, so callers layered on this package do not each
// re-implement a pre-publish failure result.
func NewErrPublishResult(err error) PublishResult {
	return &errPublishResult{err: err}
}

// stopBlocking races a publisher's blocking Stop against ctx. The underlying
// flush cannot be cancelled, so when ctx expires first the flush is left to
// finish in the background — shutdown stays bounded by the caller's deadline
// rather than stalling indefinitely.
func stopBlocking(ctx context.Context, pub *pubsub.Publisher) error {
	done := make(chan struct{})
	go func() {
		pub.Stop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
// payload, caller-supplied metadata, and any trace context propagated from ctx
// so the subscriber can continue the producer's trace. Derived wire markers
// override caller attributes. The propagator is passed in so the behaviour is
// testable without mutating global state; when ctx carries no active span
// injection is a no-op and no propagation attributes are added.
func messageAttributes(ctx context.Context, prop propagation.TextMapPropagator, msg proto.Message, custom map[string]string) map[string]string {
	attributes := make(map[string]string, len(custom)+2)
	maps.Copy(attributes, custom)
	attributes["content-type"] = "application/x-protobuf"
	attributes["schema"] = string(proto.MessageName(msg))
	if prop != nil {
		prop.Inject(ctx, propagation.MapCarrier(attributes))
	}
	return attributes
}

func (p *psPublisher[M]) Publish(ctx context.Context, msg M, options ...PublishOption) PublishResult {
	bs, err := proto.Marshal(msg)
	if err != nil {
		return &errPublishResult{err: fmt.Errorf("marshal proto: %w", err)}
	}
	opts := resolvePublishOptions(options)

	res := p.pub.Publish(ctx, &pubsub.Message{
		Data:       bs,
		Attributes: messageAttributes(ctx, p.prop, msg, opts.Attributes),
	})

	return res
}

// Stop flushes buffered messages and releases the publisher's resources,
// bounded by ctx.
func (p *psPublisher[M]) Stop(ctx context.Context) error {
	if err := stopBlocking(ctx, p.pub); err != nil {
		return fmt.Errorf("stop publisher: %w", err)
	}

	return nil
}
