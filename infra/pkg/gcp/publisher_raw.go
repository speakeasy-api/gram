package gcp

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"

	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ErrUnknownTopic is returned when a topic name cannot be resolved to a
// registered proto message, or when the message it resolves to declares no
// (gcp.pubsub.v1.topic) option. Callers should treat it as permanent: no amount
// of retrying will make an unregistered topic publishable, so the message
// belongs in a dead letter rather than a retry loop.
var ErrUnknownTopic = errors.New("unknown pubsub topic")

// RawPublisher publishes pre-marshaled bytes to a topic named at call time,
// rather than to the single topic a typed Publisher[M] is bound to.
//
// It exists for the transactional outbox, where the message was marshaled by a
// different process at a different time and the row itself names its
// destination. Prefer PubSubPublisherForMessage anywhere the topic is known at
// construction time: it is type-safe and cannot fail to resolve.
type RawPublisher interface {
	// PublishRaw publishes data to the topic declared by the proto message named
	// by topic. The attributes map is merged over the derived content-type and
	// schema markers; see NewRawPublisher for the precise merge order.
	PublishRaw(ctx context.Context, topic protoreflect.FullName, data []byte, attributes map[string]string) PublishResult
	// Stop flushes every topic publisher this instance has opened.
	Stop(ctx context.Context) error
}

// TopicResolver maps a proto full name to a zero-valued message of that type.
// Only the message's descriptor is read, so a zero value is sufficient.
type TopicResolver func(protoreflect.FullName) (proto.Message, bool)

type rawPublisherOptions struct {
	publishSettings *pubsub.PublishSettings
}

// WithRawPublishSettings applies pubsub.PublishSettings to every topic
// publisher this instance opens.
func WithRawPublishSettings(settings *pubsub.PublishSettings) func(*rawPublisherOptions) {
	return func(o *rawPublisherOptions) {
		if settings != nil {
			o.publishSettings = settings
		}
	}
}

type rawPublisher struct {
	broker   PublisherBroker
	resolve  TopicResolver
	settings *pubsub.PublishSettings

	mu         sync.Mutex
	publishers map[protoreflect.FullName]*pubsub.Publisher
}

var _ RawPublisher = (*rawPublisher)(nil)

// NewRawPublisher returns a publisher that resolves topics by proto full name
// through resolve, opening and caching one pubsub.Publisher per distinct topic.
//
// resolve is deliberately a parameter rather than a lookup in
// protoregistry.GlobalTypes. GlobalTypes only knows the message types linked
// into the running binary, so an unlinked topic would fail at publish time —
// once per message, forever — instead of at startup. Callers pass an explicit
// registry so a missing topic is a boot-time failure.
//
// Unlike the typed publisher, this one never injects trace context from the
// calling context. Callers pass traceparent in explicitly. The outbox stores
// the *producer's* trace context at write time and the relay republishes it
// much later under a trace of its own; injecting here would reparent every
// message onto the relay's trace and break the producer-to-subscriber link.
func NewRawPublisher(broker PublisherBroker, resolve TopicResolver, opts ...func(*rawPublisherOptions)) RawPublisher {
	var o rawPublisherOptions
	for _, opt := range opts {
		opt(&o)
	}

	return &rawPublisher{
		broker:     broker,
		resolve:    resolve,
		settings:   o.publishSettings,
		mu:         sync.Mutex{},
		publishers: make(map[protoreflect.FullName]*pubsub.Publisher),
	}
}

func (r *rawPublisher) PublishRaw(ctx context.Context, topic protoreflect.FullName, data []byte, attributes map[string]string) PublishResult {
	pub, prototype, err := r.publisherFor(ctx, topic)
	if err != nil {
		return &errPublishResult{err: err}
	}

	return pub.Publish(ctx, &pubsub.Message{
		Data:       data,
		Attributes: rawMessageAttributes(attributes, prototype),
	})
}

// rawMessageAttributes merges the caller's attributes with the markers derived
// from the message type.
//
// Caller attributes go on first and the derived markers over the top:
// content-type and schema describe the bytes, so a stored attribute map must
// not be able to misdeclare the wire format of the payload it travels with.
// Everything else, traceparent included, is the caller's to set — that is what
// lets an outbox row carry its producer's trace across the database hop.
func rawMessageAttributes(attributes map[string]string, prototype proto.Message) map[string]string {
	attrs := make(map[string]string, len(attributes)+2)
	maps.Copy(attrs, attributes)
	attrs["content-type"] = "application/x-protobuf"
	attrs["schema"] = string(proto.MessageName(prototype))

	return attrs
}

func (r *rawPublisher) publisherFor(ctx context.Context, topic protoreflect.FullName) (*pubsub.Publisher, proto.Message, error) {
	prototype, ok := r.resolve(topic)
	if !ok || isNilMessage(prototype) {
		return nil, nil, fmt.Errorf("resolve topic %q: %w", topic, ErrUnknownTopic)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if pub, ok := r.publishers[topic]; ok {
		return pub, prototype, nil
	}

	pub, err := r.broker.PublisherForMessage(ctx, prototype)
	if err != nil {
		// A message that declares no topic option is as unpublishable as one
		// that does not exist, so both surface as ErrUnknownTopic and both
		// dead-letter rather than retry.
		return nil, nil, fmt.Errorf("get publisher for topic %q: %w: %w", topic, ErrUnknownTopic, err)
	}
	if r.settings != nil {
		pub.PublishSettings = *r.settings
	}

	r.publishers[topic] = pub

	return pub, prototype, nil
}

// Stop flushes every cached topic publisher. Like psPublisher.Stop, the
// underlying flush cannot be cancelled, so it is raced against ctx and left to
// finish in the background if the caller's deadline expires first.
func (r *rawPublisher) Stop(ctx context.Context) error {
	r.mu.Lock()
	pubs := make([]*pubsub.Publisher, 0, len(r.publishers))
	for _, pub := range r.publishers {
		pubs = append(pubs, pub)
	}
	clear(r.publishers)
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Add(len(pubs))
		for _, pub := range pubs {
			go func() {
				defer wg.Done()
				pub.Stop()
			}()
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop raw publisher: %w", ctx.Err())
	}
}

// NoopRawPublisher discards everything published to it. It is the default in
// binaries that do not have a Pub/Sub client wired, mirroring NoopPublisher.
type NoopRawPublisher struct{}

var _ RawPublisher = (*NoopRawPublisher)(nil)

func NewNoopRawPublisher() *NoopRawPublisher {
	return &NoopRawPublisher{}
}

func (n *NoopRawPublisher) PublishRaw(ctx context.Context, topic protoreflect.FullName, data []byte, attributes map[string]string) PublishResult {
	return NewSuccessPublishResult()
}

func (n *NoopRawPublisher) Stop(context.Context) error {
	return nil
}
