// Package topics provides statically typed publishers for topics named at
// runtime.
//
// It is for callers that cannot know their destination at construction time —
// a transactional outbox, say, where a stored row records which topic to
// publish to and carries an already-marshaled payload. Naming a topic with a
// string would ordinarily mean giving up the contract gcp's publisher API
// provides: a topic is reachable only by naming the Go type that declares it.
//
// The generated registry in topics_gen.go closes that gap. Every topic name
// maps, at compile time, to the Go type generated for it, and that type — not
// the payload — is what addresses the topic. The dynamic-to-static hop happens
// once, inside the generated switch, with the type binding intact; there is
// still no way to reach an undeclared topic.
//
// The payload bytes pass through untouched. That is the property a
// transactional outbox depends on: the bytes committed with the producer's
// transaction are the bytes that reach the wire, whichever binary drains the
// row. Deciding whether they parse as the topic's message type is the topic's
// server-side BINARY schema's job — a client-side decode would be a weaker
// copy of that check, paid per message, that re-renders the payload as a side
// effect.
package topics

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"cloud.google.com/go/pubsub/v2"

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
// topic. Treat it as retryable, not permanent: writers validate names against
// the same registry, so an unknown name here almost always means the row was
// written by a binary newer than this one — a rolling deploy declared a topic
// this registry predates. The condition clears when this binary is redeployed,
// which nothing about an already-settled row would notice.
var ErrUnknownTopic = errors.New("unknown pubsub topic")

// Publisher publishes already-marshaled payloads to topics named at runtime.
// Set is the real implementation; NoopPublisher is the seam for binaries and
// tests with no Pub/Sub client, mirroring gcp.Publisher and gcp.NoopPublisher.
type Publisher interface {
	// Publish sends data to the topic named by name, verbatim. The attributes
	// map is merged under the derived content-type and schema markers, and
	// trace context is not injected from ctx — a stored message carries its
	// producer's trace context in attrs. See gcp.EncodedPublisher.
	Publish(ctx context.Context, name string, data []byte, attrs map[string]string) gcp.PublishResult
	Stop(ctx context.Context) error
}

var (
	_ Publisher = (*Set)(nil)
	_ Publisher = (*NoopPublisher)(nil)
)

// NoopPublisher discards everything published to it.
type NoopPublisher struct{}

func NewNoopPublisher() *NoopPublisher { return &NoopPublisher{} }

func (n *NoopPublisher) Publish(context.Context, string, []byte, map[string]string) gcp.PublishResult {
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
	publishers map[Topic]gcp.EncodedPublisher
}

// NewSet returns a lazily populated set of topic publishers. settings, when
// non-nil, is applied to every publisher it opens.
func NewSet(broker gcp.PublisherBroker, settings *pubsub.PublishSettings) *Set {
	return &Set{
		broker:     broker,
		settings:   settings,
		mu:         sync.Mutex{},
		publishers: make(map[Topic]gcp.EncodedPublisher),
	}
}

// Publish sends an already-marshaled payload to the named topic. An undeclared
// name yields ErrUnknownTopic without contacting Pub/Sub.
func (s *Set) Publish(ctx context.Context, name string, data []byte, attrs map[string]string) gcp.PublishResult {
	topic, ok := Lookup(name)
	if !ok {
		return failedResult{err: fmt.Errorf("%w: %s", ErrUnknownTopic, name)}
	}

	pub, err := s.publisherFor(ctx, topic)
	if err != nil {
		return failedResult{err: err}
	}

	return pub.PublishEncoded(ctx, data, attrs)
}

func (s *Set) publisherFor(ctx context.Context, topic Topic) (gcp.EncodedPublisher, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if pub, ok := s.publishers[topic]; ok {
		return pub, nil
	}

	// A failure here is deliberately not marked permanent. Undeclared topics
	// were ruled out by Lookup, which leaves the broker failing to reach
	// Pub/Sub: the emulator reconciles the topic over the network before
	// handing a publisher back, and a cancelled context surfaces here too.
	// Marking those permanent would dead-letter a perfectly valid row over a
	// blip or a shutdown.
	pub, err := newPublisher(ctx, s.broker, topic, s.settings)
	if err != nil {
		return nil, fmt.Errorf("create publisher for topic %s: %w", topic, err)
	}

	s.publishers[topic] = pub

	return pub, nil
}

// Stop flushes every publisher this set has opened.
func (s *Set) Stop(ctx context.Context) error {
	s.mu.Lock()
	pubs := make([]gcp.EncodedPublisher, 0, len(s.publishers))
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
