// Package topics provides statically typed publishers for topics named at
// runtime.
//
// It is for callers that cannot know their destination at construction time —
// a transactional outbox, say, where a stored row records which topic to
// publish to and carries an already-marshaled payload. Naming a topic with a
// string would ordinarily mean giving up the type safety gcp.Publisher[M]
// provides, since the publish path would no longer know what it was carrying.
//
// The generated registry in topics_gen.go closes that gap: every topic name
// maps, at compile time, to the Go type generated for it, and a payload is
// decoded as that type before being published. So whatever goes on the wire is
// an instance of the topic's declared message, and corrupt bytes are rejected
// here rather than handed to Pub/Sub — including against the emulator, which
// does no schema validation. Protobuf tolerates unknown fields, so this is not
// a guarantee that the bytes were originally written as this type; it is a
// guarantee about what is published.
//
// This package builds strictly on top of gcp.PubSubPublisherForMessage. It adds
// no way to publish something the typed API could not already publish.
package topics

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

// failedResult reports a failure that happened before the message reached
// Pub/Sub. It is defined here rather than reusing an unexported helper from the
// gcp package so that package stays untouched by this one.
type failedResult struct{ err error }

func (r failedResult) Ready() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (r failedResult) Get(context.Context) (string, error) { return "", r.err }

// ErrUnknownTopic is returned when a name does not correspond to any declared
// topic. Treat it as permanent: an undeclared topic will not become declared
// without a deploy, and a deploy re-reads the row anyway.
var ErrUnknownTopic = errors.New("unknown pubsub topic")

// ErrPayloadMismatch is returned when a payload does not decode as the topic's
// message type. Also permanent — the stored bytes will not start matching.
var ErrPayloadMismatch = errors.New("payload does not match topic message type")

// EncodedPublisher publishes an already-marshaled payload to a single topic.
type EncodedPublisher interface {
	// PublishEncoded decodes data as the topic's message type and publishes it.
	// Trace context is taken from ctx, so a caller republishing a stored message
	// should extract the producer's context first.
	PublishEncoded(ctx context.Context, data []byte) gcp.PublishResult
	Stop(ctx context.Context) error
}

// encodedPublisher binds a topic's message type to its publisher. M is fixed at
// construction by the generated registry, which is what keeps the decode honest.
type encodedPublisher[M proto.Message] struct {
	zero M
	pub  gcp.Publisher[M]
}

func newEncodedPublisher[M proto.Message](ctx context.Context, broker gcp.PublisherBroker, zero M, settings *pubsub.PublishSettings) (EncodedPublisher, error) {
	// WithPubSubPublishSettings ignores a nil argument, so this covers both the
	// tuned and default cases without the caller branching.
	pub, err := gcp.PubSubPublisherForMessage(ctx, broker, zero, gcp.WithPubSubPublishSettings(settings))
	if err != nil {
		return nil, fmt.Errorf("create publisher for %s: %w", proto.MessageName(zero), err)
	}

	return &encodedPublisher[M]{zero: zero, pub: pub}, nil
}

func (e *encodedPublisher[M]) PublishEncoded(ctx context.Context, data []byte) gcp.PublishResult {
	msg, ok := e.zero.ProtoReflect().New().Interface().(M)
	if !ok {
		return failedResult{err: fmt.Errorf("%w: cannot instantiate %s", ErrPayloadMismatch, proto.MessageName(e.zero))}
	}

	if err := proto.Unmarshal(data, msg); err != nil {
		return failedResult{err: fmt.Errorf("%w: %s: %w", ErrPayloadMismatch, proto.MessageName(e.zero), err)}
	}

	return e.pub.Publish(ctx, msg)
}

func (e *encodedPublisher[M]) Stop(ctx context.Context) error {
	if err := e.pub.Stop(ctx); err != nil {
		return fmt.Errorf("stop publisher for %s: %w", proto.MessageName(e.zero), err)
	}

	return nil
}

// Publisher publishes already-marshaled payloads to topics named at runtime.
// Set is the real implementation; NoopPublisher is the seam for binaries and
// tests with no Pub/Sub client, mirroring gcp.Publisher and gcp.NoopPublisher.
type Publisher interface {
	Publish(ctx context.Context, name string, data []byte) gcp.PublishResult
	Stop(ctx context.Context) error
}

var (
	_ Publisher = (*Set)(nil)
	_ Publisher = (*NoopPublisher)(nil)
)

// NoopPublisher discards everything published to it.
type NoopPublisher struct{}

func NewNoopPublisher() *NoopPublisher { return &NoopPublisher{} }

func (n *NoopPublisher) Publish(context.Context, string, []byte) gcp.PublishResult {
	return gcp.NewSuccessPublishResult()
}

func (n *NoopPublisher) Stop(context.Context) error { return nil }

// Set holds one publisher per topic, built on first use.
//
// Publishers are created lazily rather than up front because a caller choosing
// topics at runtime has no way to know which ones it will use, and opening a
// publisher for every declared topic would create client state for topics it
// never touches.
type Set struct {
	broker   gcp.PublisherBroker
	settings *pubsub.PublishSettings

	mu         sync.Mutex
	publishers map[Topic]EncodedPublisher
}

// NewSet returns a lazily populated set of topic publishers. settings, when
// non-nil, is applied to every publisher it opens.
func NewSet(broker gcp.PublisherBroker, settings *pubsub.PublishSettings) *Set {
	return &Set{
		broker:     broker,
		settings:   settings,
		mu:         sync.Mutex{},
		publishers: make(map[Topic]EncodedPublisher),
	}
}

// Publish sends an already-marshaled payload to the named topic. An undeclared
// name yields ErrUnknownTopic without contacting Pub/Sub.
func (s *Set) Publish(ctx context.Context, name string, data []byte) gcp.PublishResult {
	topic, ok := Lookup(name)
	if !ok {
		return failedResult{err: fmt.Errorf("%w: %s", ErrUnknownTopic, name)}
	}

	pub, err := s.publisherFor(ctx, topic)
	if err != nil {
		return failedResult{err: err}
	}

	return pub.PublishEncoded(ctx, data)
}

func (s *Set) publisherFor(ctx context.Context, topic Topic) (EncodedPublisher, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if pub, ok := s.publishers[topic]; ok {
		return pub, nil
	}

	pub, err := newPublisher(ctx, s.broker, topic, s.settings)
	if err != nil {
		return nil, err
	}

	s.publishers[topic] = pub

	return pub, nil
}

// Stop flushes every publisher this set has opened.
func (s *Set) Stop(ctx context.Context) error {
	s.mu.Lock()
	pubs := make([]EncodedPublisher, 0, len(s.publishers))
	for _, pub := range s.publishers {
		pubs = append(pubs, pub)
	}
	clear(s.publishers)
	s.mu.Unlock()

	var errs []error
	for _, pub := range pubs {
		if err := pub.Stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
