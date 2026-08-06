package gcp

import (
	"context"
	"fmt"
	"maps"

	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/protobuf/proto"
)

// EncodedPublisher publishes pre-marshaled bytes to the single topic it was
// constructed for. It exists for callers that hold a payload marshaled by a
// different process at a different time — a transactional outbox row, say —
// and must put those bytes on the wire verbatim. Prefer Publisher[M] anywhere
// a value is in hand: it derives the bytes itself and cannot be handed a
// payload that was never an M.
type EncodedPublisher interface {
	// PublishEncoded publishes data untouched. The attributes map is merged
	// under the derived content-type and schema markers, and trace context is
	// deliberately NOT injected from ctx: a stored message carries its
	// producer's trace context in attributes, and injecting here would
	// reparent it onto the republisher's trace, breaking the
	// producer-to-subscriber link.
	PublishEncoded(ctx context.Context, data []byte, attributes map[string]string) PublishResult
	// Stop flushes buffered messages and releases the publisher's resources.
	Stop(ctx context.Context) error
}

type psEncodedPublisherOptions struct {
	publishSettings *pubsub.PublishSettings
}

// WithEncodedPublishSettings applies pubsub.PublishSettings to the publisher.
// A nil argument is ignored, so callers can pass through an optional settings
// value without branching.
func WithEncodedPublishSettings(settings *pubsub.PublishSettings) func(*psEncodedPublisherOptions) {
	return func(o *psEncodedPublisherOptions) {
		if settings != nil {
			o.publishSettings = settings
		}
	}
}

type psEncodedPublisher[M proto.Message] struct {
	// zero is retained so the derived content-type and schema attributes stay
	// available without ever decoding a payload.
	zero M
	pub  *pubsub.Publisher
}

// PubSubEncodedPublisherForMessage returns a publisher that carries
// pre-marshaled bytes to the topic declared by M's (gcp.pubsub.v1.topic)
// option. It errors if msg does not declare a topic.
//
// M supplies the destination, not a value to be reconstructed: the payload is
// never unmarshaled, so the bytes handed to PublishEncoded are the bytes that
// reach the topic. Validating that they parse as M is the topic's server-side
// BINARY schema's job; a client-side decode would be a weaker copy of that
// check bought by re-rendering the payload.
func PubSubEncodedPublisherForMessage[M proto.Message](ctx context.Context, broker PublisherBroker, msg M, opts ...func(*psEncodedPublisherOptions)) (EncodedPublisher, error) {
	if isNilMessage(msg) {
		return nil, fmt.Errorf("message must not be nil")
	}

	publisher, err := broker.PublisherForMessage(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("get publisher for message: %w", err)
	}

	var o psEncodedPublisherOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.publishSettings != nil {
		publisher.PublishSettings = *o.publishSettings
	}

	return &psEncodedPublisher[M]{zero: msg, pub: publisher}, nil
}

// encodedMessageAttributes merges the caller's attributes with the markers
// derived from the message type.
//
// Caller attributes go on first and the derived markers over the top:
// content-type and schema describe the bytes, so a stored attribute map must
// not be able to misdeclare the wire format of the payload it travels with.
// Everything else, traceparent included, is the caller's to set — that is what
// lets an outbox row carry its producer's trace across the database hop.
func encodedMessageAttributes(attributes map[string]string, zero proto.Message) map[string]string {
	attrs := make(map[string]string, len(attributes)+2)
	maps.Copy(attrs, attributes)
	attrs["content-type"] = "application/x-protobuf"
	attrs["schema"] = string(proto.MessageName(zero))

	return attrs
}

func (p *psEncodedPublisher[M]) PublishEncoded(ctx context.Context, data []byte, attributes map[string]string) PublishResult {
	return p.pub.Publish(ctx, &pubsub.Message{
		Data:       data,
		Attributes: encodedMessageAttributes(attributes, p.zero),
	})
}

// Stop flushes buffered messages and releases the publisher's resources. Like
// psPublisher.Stop, the underlying flush cannot be cancelled, so it runs in a
// goroutine raced against ctx and is left to finish in the background if the
// caller's deadline expires first.
func (p *psEncodedPublisher[M]) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.pub.Stop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop encoded publisher: %w", ctx.Err())
	}
}
