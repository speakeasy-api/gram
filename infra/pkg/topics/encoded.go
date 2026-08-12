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

// ErrUnknownTopic is returned when a name does not correspond to any declared
// topic. Treat it as retryable, not permanent: writers validate names against
// the same registry, so an unknown name here almost always means the row was
// written by a binary newer than this one — a rolling deploy declared a topic
// this registry predates. The condition clears when this binary is redeployed,
// which nothing about an already-settled row would notice.
var ErrUnknownTopic = errors.New("unknown pubsub topic")

// Publisher publishes already-marshaled payloads to topics named at runtime.
// Mux is the real implementation; NoopPublisher is the seam for binaries and
// tests with no Pub/Sub client, mirroring gcp.Publisher and gcp.NoopPublisher.
type Publisher interface {
	// Publish sends data to the topic named by name, verbatim. The slice is
	// retained until the returned result resolves and must not be modified by
	// the caller until then. The attributes map is merged under the derived
	// content-type and schema markers, and trace context is not injected from
	// ctx — a stored message carries its producer's trace context in attrs.
	// See gcp.EncodedPublisher.
	Publish(ctx context.Context, name string, data []byte, attrs map[string]string) gcp.PublishResult
	Stop(ctx context.Context) error
}

var (
	_ Publisher = (*Mux)(nil)
	_ Publisher = (*NoopPublisher)(nil)
)

// NoopPublisher discards everything published to it.
type NoopPublisher struct{}

func NewNoopPublisher() *NoopPublisher { return &NoopPublisher{} }

func (n *NoopPublisher) Publish(context.Context, string, []byte, map[string]string) gcp.PublishResult {
	return gcp.NewSuccessPublishResult()
}

func (n *NoopPublisher) Stop(context.Context) error { return nil }

// Mux routes each publish to a per-topic publisher, built on first use.
//
// Publishers are created lazily rather than up front because a caller choosing
// topics at runtime has no way to know which ones it will use, and opening a
// publisher for every declared topic would create client state for topics it
// never touches.
type Mux struct {
	broker   gcp.PublisherBroker
	settings *pubsub.PublishSettings

	// mu guards the map and the closed flag; each entry carries its own lock
	// so one topic's construction never serialises another's.
	mu         sync.Mutex
	publishers map[Topic]*topicPublisher
	// closed refuses publishers once Stop has begun. Without it, a publish
	// racing Stop would repopulate the just-cleared map with a publisher
	// nothing ever flushes, and its messages would sit in a buffer no
	// shutdown knows about.
	closed bool
}

// topicPublisher guards one topic's lazily built publisher. Construction is
// not a pure lookup — the emulator broker reconciles the topic over the
// network before handing a publisher back — so the lock held across it must be
// per topic: callers of the same topic wait for the one build in flight,
// callers of other topics proceed.
type topicPublisher struct {
	mu  sync.Mutex
	pub gcp.EncodedPublisher
}

// NewMux returns a lazily populated mux of topic publishers. settings, when
// non-nil, is applied to every publisher it opens.
func NewMux(broker gcp.PublisherBroker, settings *pubsub.PublishSettings) *Mux {
	return &Mux{
		broker:     broker,
		settings:   settings,
		mu:         sync.Mutex{},
		publishers: make(map[Topic]*topicPublisher),
		closed:     false,
	}
}

// Warm eagerly builds a publisher for every declared topic, so a broker that
// cannot hand one back fails here — at boot, naming the topic — rather than
// one outbox row at a time through a retry budget. On the emulator broker it
// also reconciles each topic as a side effect, which is what creates them in
// local dev before anything publishes. The publishers are cached, so warming
// costs nothing that first use would not have.
func (m *Mux) Warm(ctx context.Context) error {
	for _, topic := range All() {
		if _, err := m.publisherFor(ctx, topic); err != nil {
			return err
		}
	}

	return nil
}

// Publish sends an already-marshaled payload to the named topic. An undeclared
// name yields ErrUnknownTopic without contacting Pub/Sub.
func (m *Mux) Publish(ctx context.Context, name string, data []byte, attrs map[string]string) gcp.PublishResult {
	topic, ok := Lookup(name)
	if !ok {
		return gcp.NewErrPublishResult(fmt.Errorf("%w: %s", ErrUnknownTopic, name))
	}

	pub, err := m.publisherFor(ctx, topic)
	if err != nil {
		return gcp.NewErrPublishResult(err)
	}

	return pub.PublishEncoded(ctx, data, attrs)
}

func (m *Mux) publisherFor(ctx context.Context, topic Topic) (gcp.EncodedPublisher, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		// Not permanent: the mux is stopping because the process is, and the
		// row will be retried by whichever process claims it next.
		return nil, fmt.Errorf("publisher mux is stopped: cannot publish to %s", topic)
	}
	tp, ok := m.publishers[topic]
	if !ok {
		tp = &topicPublisher{mu: sync.Mutex{}, pub: nil}
		m.publishers[topic] = tp
	}
	m.mu.Unlock()

	tp.mu.Lock()
	defer tp.mu.Unlock()

	if tp.pub != nil {
		return tp.pub, nil
	}

	// A failure here is deliberately not marked permanent, and not cached:
	// undeclared topics were ruled out by Lookup, which leaves the broker
	// failing to reach Pub/Sub — the emulator reconciles the topic over the
	// network before handing a publisher back, and a cancelled context
	// surfaces here too. Marking those permanent would dead-letter a
	// perfectly valid row over a blip or a shutdown; caching them would make
	// the blip last forever.
	pub, err := newPublisher(ctx, m.broker, topic, m.settings)
	if err != nil {
		return nil, fmt.Errorf("create publisher for topic %s: %w", topic, err)
	}

	tp.pub = pub

	return pub, nil
}

// Stop flushes every publisher this mux has opened. Flushes run concurrently
// so shutdown is bounded by the slowest one rather than their sum: under a
// shared deadline, a sequential loop would let one slow topic starve the rest
// of their flush window, abandoning buffered messages that then get re-claimed
// and re-published on the next drain.
func (m *Mux) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	tps := make([]*topicPublisher, 0, len(m.publishers))
	for _, tp := range m.publishers {
		tps = append(tps, tp)
	}
	clear(m.publishers)
	m.mu.Unlock()

	errs := make([]error, len(tps))
	var wg sync.WaitGroup
	for i, tp := range tps {
		wg.Go(func() {
			// Waits out any construction still in flight for this topic, then
			// takes ownership so a publisher is stopped exactly once.
			tp.mu.Lock()
			pub := tp.pub
			tp.pub = nil
			tp.mu.Unlock()

			if pub != nil {
				errs[i] = pub.Stop(ctx)
			}
		})
	}
	wg.Wait()

	return errors.Join(errs...)
}
