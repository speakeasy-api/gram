package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/speakeasy-api/gram/infra/internal/attr"
	"google.golang.org/protobuf/proto"
)

const (
	// defaultBatchMaxMessages is the buffer size used by ReceiveBatch when
	// BatchReceiveSettings.MaxMessages is unset.
	defaultBatchMaxMessages = 100
	// defaultBatchMaxLatency is the maximum time a partial batch waits before
	// being flushed when BatchReceiveSettings.MaxLatency is unset.
	defaultBatchMaxLatency = time.Second
)

// BatchReceiveSettings tunes how Subscriber.ReceiveBatch groups messages. A
// batch is flushed when either MaxMessages have buffered or MaxLatency elapses,
// whichever happens first.
type BatchReceiveSettings struct {
	// MaxMessages is the number of buffered messages that triggers a flush. A
	// value <= 0 falls back to defaultBatchMaxMessages.
	MaxMessages int
	// MaxLatency is how long a partial batch waits before being flushed. A value
	// <= 0 falls back to defaultBatchMaxLatency. It must stay well below the
	// subscription's ack deadline (and the receiver's MaxExtension) since
	// buffered messages remain outstanding until the batch is acked.
	MaxLatency time.Duration
}

type SubscriberBroker interface {
	SubscriberForMessage(ctx context.Context, msg proto.Message, subscription proto.Message) (*pubsub.Subscriber, error)
}

type psSubscriberOptions struct {
	receiveSettings *pubsub.ReceiveSettings
	logger          *slog.Logger
}

func WithPubSubReceiveSettings(settings *pubsub.ReceiveSettings) func(*psSubscriberOptions) {
	return func(opts *psSubscriberOptions) {
		opts.receiveSettings = settings
	}
}

// WithSubscriberLogger sets the logger used to report panics recovered while
// processing messages. When unset the subscriber falls back to slog.Default().
func WithSubscriberLogger(logger *slog.Logger) func(*psSubscriberOptions) {
	return func(opts *psSubscriberOptions) {
		if logger != nil {
			opts.logger = logger
		}
	}
}

type PubSubAckResult interface {
	Ready() <-chan struct{}
	Get(ctx context.Context) (res pubsub.AcknowledgeStatus, err error)
}

type MessageMetadata struct {
	// ID is the unique identifier for the message, assigned by message broker.
	ID string
	// Attributes contains the attributes of the message that were carried along
	// with the payload.
	Attributes map[string]string
	// DeliveryAttempt is the number of times a message has been delivered. This
	// is part of the dead lettering feature that forwards messages that fail to
	// be processed (from nack/ack deadline timeout) to a dead letter topic. If
	// dead lettering is enabled, this will be set on all attempts, starting
	// with value 1. Otherwise, the value will be nil.
	DeliveryAttempt *int
}

type Subscriber[M proto.Message] interface {
	Receive(context.Context, func(context.Context, M, MessageMetadata) error) error
	ReceiveBatch(context.Context, BatchReceiveSettings, func(context.Context, []M, []MessageMetadata) error) error
}

// incomingMessage decouples per-message processing from the concrete
// *pubsub.Message so the handling logic (including panic recovery) can be
// exercised in tests without a live subscriber.
type incomingMessage struct {
	id              string
	data            []byte
	attributes      map[string]string
	deliveryAttempt *int
	ack             func()
	nack            func()
}

type psSubscriber[M proto.Message] struct {
	sub                   *pubsub.Subscriber
	new                   func() M
	logger                *slog.Logger
	topicProtoName        string
	subscriptionProtoName string
}

func (s *psSubscriber[M]) Receive(ctx context.Context, f func(context.Context, M, MessageMetadata) error) error {
	err := s.sub.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
		s.handle(ctx, incomingMessage{
			id:              m.ID,
			data:            m.Data,
			attributes:      m.Attributes,
			deliveryAttempt: m.DeliveryAttempt,
			ack:             m.Ack,
			nack:            m.Nack,
		}, f)
	})
	if err != nil {
		return fmt.Errorf("receive: %w", err)
	}

	return nil
}

func (s *psSubscriber[M]) handle(ctx context.Context, m incomingMessage, f func(context.Context, M, MessageMetadata) error) {
	defer m.ack()

	// Recover from panics in the handler so a single bad message does not take
	// down the receive goroutine. Log with diagnostic context so the crash is
	// visible instead of becoming a silent redelivery loop, then nack so the
	// message is redelivered (and eventually dead-lettered if it keeps
	// panicking).
	defer func() {
		if r := recover(); r != nil {
			deliveryAttempt := 0
			if m.deliveryAttempt != nil {
				deliveryAttempt = *m.deliveryAttempt
			}

			s.logger.ErrorContext(ctx, "panic recovered while processing pubsub message",
				attr.SlogErrorMessage(fmt.Sprintf("%v", r)),
				attr.SlogErrorStack(string(debug.Stack())),
				attr.SlogTopicProtoName(s.topicProtoName),
				attr.SlogSubscriptionProtoName(s.subscriptionProtoName),
				slog.String("message_id", m.id),
				slog.Int("delivery_attempt", deliveryAttempt),
			)
			m.nack()
		}
	}()

	msg := s.new()
	if err := proto.Unmarshal(m.data, msg); err != nil {
		m.nack()
		return
	}
	if err := f(ctx, msg, MessageMetadata{
		ID:              m.id,
		Attributes:      m.attributes,
		DeliveryAttempt: m.deliveryAttempt,
	}); err != nil {
		m.nack()
		return
	}
}

// ReceiveBatch consumes messages and delivers them to f in batches. The
// underlying pubsub client only delivers one message per callback, so messages
// are buffered here and flushed as a slice once settings.MaxMessages are
// buffered or settings.MaxLatency elapses, whichever comes first.
//
// Ack/nack is all-or-nothing: when f returns nil the whole batch is acked; when
// f returns an error (or panics) the whole batch is nacked. Messages that fail
// to unmarshal are nacked individually and excluded from the batch handed to f.
func (s *psSubscriber[M]) ReceiveBatch(ctx context.Context, settings BatchReceiveSettings, f func(context.Context, []M, []MessageMetadata) error) error {
	return s.batchLoop(ctx, settings, func(ctx context.Context, deliver func(incomingMessage)) error {
		return s.sub.Receive(ctx, func(_ context.Context, m *pubsub.Message) {
			deliver(incomingMessage{
				id:              m.ID,
				data:            m.Data,
				attributes:      m.Attributes,
				deliveryAttempt: m.DeliveryAttempt,
				ack:             m.Ack,
				nack:            m.Nack,
			})
		})
	}, f)
}

// batchLoop holds the buffering, size/latency flushing, and drain logic shared
// by ReceiveBatch. The delivery source is abstracted behind receive so the loop
// can be exercised in tests (with a synthetic source and a fake clock) without a
// live pubsub subscriber: receive must call deliver once per incoming message
// and return when ctx is cancelled (or on a terminal error).
func (s *psSubscriber[M]) batchLoop(
	ctx context.Context,
	settings BatchReceiveSettings,
	receive func(ctx context.Context, deliver func(incomingMessage)) error,
	f func(context.Context, []M, []MessageMetadata) error,
) error {
	size := settings.MaxMessages
	if size <= 0 {
		size = defaultBatchMaxMessages
	}
	latency := settings.MaxLatency
	if latency <= 0 {
		latency = defaultBatchMaxLatency
	}

	newBuf := func() []incomingMessage { return make([]incomingMessage, 0, size) }

	var mu sync.Mutex
	pending := newBuf()

	take := func() []incomingMessage {
		mu.Lock()
		defer mu.Unlock()
		batch := pending
		pending = newBuf()
		return batch
	}

	flush := func() {
		if batch := take(); len(batch) > 0 {
			s.handleBatch(ctx, batch, f)
		}
	}

	// Flush partial batches on a timer so messages are not held longer than the
	// configured latency (and beyond their ack deadline) under low throughput.
	ticker := time.NewTicker(latency)
	defer ticker.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				flush()
			}
		}
	}()

	err := receive(ctx, func(m incomingMessage) {
		mu.Lock()
		pending = append(pending, m)
		var batch []incomingMessage
		if len(pending) >= size {
			batch = pending
			pending = newBuf()
		}
		mu.Unlock()

		if len(batch) > 0 {
			s.handleBatch(ctx, batch, f)
		}
	})

	// Drain any messages still buffered when receive returns so they are acked
	// or nacked rather than silently dropped.
	flush()

	if err != nil {
		return fmt.Errorf("receive batch: %w", err)
	}

	return nil
}

func (s *psSubscriber[M]) handleBatch(ctx context.Context, batch []incomingMessage, f func(context.Context, []M, []MessageMetadata) error) {
	msgs := make([]M, 0, len(batch))
	metas := make([]MessageMetadata, 0, len(batch))
	valid := make([]incomingMessage, 0, len(batch))

	logger := s.logger.With(
		attr.SlogTopicProtoName(s.topicProtoName),
		attr.SlogSubscriptionProtoName(s.subscriptionProtoName),
	)

	for _, m := range batch {
		msg := s.new()
		if err := proto.Unmarshal(m.data, msg); err != nil {
			logger.ErrorContext(
				ctx,
				"failed to unmarshal pubsub message",
				attr.SlogError(err),
				attr.SlogSubscriberMessageID(m.id),
				attr.SlogSubscriberDeliveryAttempt(m.deliveryAttempt),
			)
			m.nack()
			continue
		}
		msgs = append(msgs, msg)
		metas = append(metas, MessageMetadata{
			ID:              m.id,
			Attributes:      m.attributes,
			DeliveryAttempt: m.deliveryAttempt,
		})
		valid = append(valid, m)
	}

	if len(valid) == 0 {
		return
	}

	ackAll := func() {
		for _, m := range valid {
			m.ack()
		}
	}
	nackAll := func() {
		for _, m := range valid {
			m.nack()
		}
	}

	// Registered first so it runs last: on the happy path it acks the batch, and
	// after a nack it is a no-op since pubsub honours the first ack/nack per
	// message.
	defer ackAll()

	defer func() {
		if r := recover(); r != nil {
			logger.ErrorContext(ctx, "panic recovered while processing pubsub message batch",
				attr.SlogErrorMessage(fmt.Sprintf("%v", r)),
				attr.SlogErrorStack(string(debug.Stack())),
				attr.SlogTopicProtoName(s.topicProtoName),
				attr.SlogSubscriptionProtoName(s.subscriptionProtoName),
				attr.SlogSubscriberMessageID(valid[0].id),
				attr.SlogSubscriberBatchSize(len(valid)),
			)
			nackAll()
		}
	}()

	if err := f(ctx, msgs, metas); err != nil {
		nackAll()
		return
	}
}

type SubscriberOption func(*psSubscriberOptions)

func PubSubSubscriberForMessage[M proto.Message](ctx context.Context, broker SubscriberBroker, msg M, subscription proto.Message, options ...SubscriberOption) (Subscriber[M], error) {
	if isNilMessage(msg) {
		return nil, fmt.Errorf("message must not be nil")
	}
	if isNilMessage(subscription) {
		return nil, fmt.Errorf("subscription marker message must not be nil")
	}

	descriptor := subscription.ProtoReflect().Descriptor()
	msgref := msg.ProtoReflect()

	if _, ok := msgref.New().Interface().(M); !ok {
		return nil, fmt.Errorf("proto message %s cannot be constructed as %T", descriptor.FullName(), msg)
	}

	sub, err := broker.SubscriberForMessage(ctx, msg, subscription)
	if err != nil {
		return nil, fmt.Errorf("get subscriber for message: %w", err)
	}

	var opts psSubscriberOptions
	for _, opt := range options {
		opt(&opts)
	}
	if opts.receiveSettings != nil {
		sub.ReceiveSettings = *opts.receiveSettings
	}
	logger := opts.logger
	if logger == nil {
		logger = slog.Default()
	}

	mt := msgref.Type()
	return &psSubscriber[M]{
		sub:                   sub,
		new:                   func() M { return mt.New().Interface().(M) },
		logger:                logger,
		topicProtoName:        string(msgref.Descriptor().FullName()),
		subscriptionProtoName: string(descriptor.FullName()),
	}, nil
}
