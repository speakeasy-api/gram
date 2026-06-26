package gcp

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
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
// batch is flushed when MaxMessages have buffered, the buffered payloads reach
// MaxBytes, or MaxLatency elapses, whichever happens first.
type BatchReceiveSettings struct {
	// MaxMessages is the number of buffered messages that triggers a flush. A
	// value <= 0 falls back to defaultBatchMaxMessages. It bounds how many
	// messages are held in memory, and left un-acked, per batch. It is
	// independent of the receiver's ReceiveSettings.MaxOutstandingMessages: the
	// underlying client releases that limit (really a concurrency cap on in-flight
	// receive callbacks) when our callback returns after buffering, not when the
	// message is acked, so buffering does not consume outstanding slots and the
	// buffer can always reach MaxMessages regardless of the outstanding limit.
	MaxMessages int
	// MaxBytes is the combined size of buffered message payloads, in bytes, that
	// triggers a flush. A value <= 0 disables byte-based flushing, leaving
	// MaxMessages and MaxLatency as the only triggers. Like MaxMessages it is a
	// trigger, not a hard cap: a single payload at or above MaxBytes flushes on
	// its own, and a batch can overshoot MaxBytes by up to the size of the
	// message that crossed the threshold. Memory is bounded by the receiver's
	// ReceiveSettings.MaxOutstandingBytes, not by this setting.
	MaxBytes int
	// MaxLatency is how long a partial batch waits before being flushed. A value
	// <= 0 falls back to defaultBatchMaxLatency. It must stay well below the
	// receiver's ReceiveSettings.MaxExtension (default 60m) — the ceiling up to
	// which the client auto-extends a buffered message's ack deadline. The
	// subscription's own ackDeadlineSeconds is not the limit: the client keeps
	// the lease alive past it via modacks. But buffered messages remain leased
	// (and un-acked) until their batch flushes, so once MaxLatency crosses
	// MaxExtension the client stops extending, pubsub redelivers them before the
	// batch flushes, and the eventual ack lands on a stale copy.
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
			s.logger.ErrorContext(ctx, "panic recovered while processing pubsub message",
				attr.SlogErrorMessage(fmt.Sprintf("%v", r)),
				attr.SlogErrorStack(string(debug.Stack())),
				attr.SlogTopicProtoName(s.topicProtoName),
				attr.SlogSubscriptionProtoName(s.subscriptionProtoName),
				attr.SlogSubscriberMessageID(m.id),
				attr.SlogSubscriberDeliveryAttempt(m.deliveryAttempt),
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
// buffered, their payloads reach settings.MaxBytes, or settings.MaxLatency
// elapses, whichever comes first.
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

// batchLoop holds the buffering, count/byte/latency flushing, and drain logic
// shared by ReceiveBatch. The latency timer is armed when a message starts a new
// batch and stopped when the batch is detached, so the window is measured from
// each batch's first message. On cancellation a drainer goroutine flushes the
// in-flight batch while receive is still running, because ack/nack must reach
// the live iterator: Subscriber.Receive only returns once every outstanding
// message is acked/nacked, so the drain cannot wait until after it returns. The
// delivery source is abstracted behind receive so the loop can be exercised in
// tests (with a synthetic source and a fake clock) without a live pubsub
// subscriber: receive must call deliver once per incoming message and return
// when ctx is cancelled (or on a terminal error).
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
	// maxBytes is an optional trigger: a value <= 0 leaves count and latency as
	// the only flush conditions.
	maxBytes := settings.MaxBytes

	newBuf := func() []incomingMessage { return make([]incomingMessage, 0, size) }

	var mu sync.Mutex
	pending := newBuf()
	pendingBytes := 0
	// flushTimer bounds how long the oldest buffered message waits. It is armed
	// when a message starts a new batch and stopped whenever the batch is
	// detached, so the latency window is measured from the first message of each
	// batch (matching google.golang.org/api/support/bundler) rather than from a
	// free-running clock. A nil flushTimer means no batch is currently waiting.
	var flushTimer *time.Timer
	// timerWG tracks in-flight timer callbacks so shutdown can wait for one that
	// is mid-flush before draining. Every armed timer adds one; the count is
	// balanced either by the callback's deferred Done or, when Stop cancels the
	// callback before it runs, by the stopping goroutine.
	var timerWG sync.WaitGroup

	// take detaches the buffered messages under the lock and stops the latency
	// timer. It returns nil (without reallocating) when nothing is buffered so a
	// no-op flush does not churn a fresh size-capacity buffer.
	take := func() []incomingMessage {
		mu.Lock()
		defer mu.Unlock()
		if len(pending) == 0 {
			return nil
		}
		batch := pending
		pending = newBuf()
		pendingBytes = 0
		if flushTimer != nil {
			// Stop reports true when it cancels the callback before it runs; in that
			// case balance the Add here since the callback's Done will not fire.
			if flushTimer.Stop() {
				timerWG.Done()
			}
			flushTimer = nil
		}
		return batch
	}

	// handlerMu serializes handleBatch so the latency callback and the
	// size/byte-trigger path never invoke f concurrently. Buffering (under mu)
	// continues while a batch is in flight; only the handler call is serialized.
	var handlerMu sync.Mutex
	runBatch := func(batch []incomingMessage) {
		if len(batch) == 0 {
			return
		}
		handlerMu.Lock()
		defer handlerMu.Unlock()
		s.handleBatch(ctx, batch, f)
	}
	flush := func() { runBatch(take()) }

	// draining is flipped on cancellation so that, during the underlying client's
	// graceful shutdown, every message the receiver is still delivering flushes
	// inline instead of waiting out MaxLatency. The cancellation drain itself MUST
	// run while receive is still in flight: Subscriber.Receive does not return
	// until every outstanding message is acked/nacked, and once it returns the
	// iterator is torn down so ack/nack become no-ops. Draining only after receive
	// returns would deadlock that wait until the messages expire (up to
	// MaxExtension) and then redeliver the batch instead of acking it.
	var draining atomic.Bool
	stopDrainer := make(chan struct{})
	drainerDone := make(chan struct{})
	go func() {
		defer close(drainerDone)
		select {
		case <-ctx.Done():
			draining.Store(true)
			flush()
		case <-stopDrainer:
			// receive returned without ctx being cancelled (e.g. a stream error);
			// there is nothing to drain into a live iterator.
		}
	}()

	err := receive(ctx, func(m incomingMessage) {
		mu.Lock()
		pending = append(pending, m)
		pendingBytes += len(m.data)
		// draining.Load() forces an immediate flush so messages delivered during
		// shutdown are acked inline rather than buffered behind the latency timer.
		full := len(pending) >= size || (maxBytes > 0 && pendingBytes >= maxBytes) || draining.Load()
		if !full && flushTimer == nil {
			// This message starts a new batch: arm the timer for its lifetime. The
			// callback captures its own timer and no-ops if a size/byte flush has
			// since superseded it, so a stale firing never flushes the next batch
			// early.
			timerWG.Add(1)
			var t *time.Timer
			t = time.AfterFunc(latency, func() {
				defer timerWG.Done()
				mu.Lock()
				if flushTimer != t || len(pending) == 0 {
					mu.Unlock()
					return
				}
				batch := pending
				pending = newBuf()
				pendingBytes = 0
				flushTimer = nil
				mu.Unlock()
				runBatch(batch)
			})
			flushTimer = t
		}
		mu.Unlock()

		if full {
			flush()
		}
	})

	// receive has returned. Stop a pending timer and wait for any in-flight timer
	// callback, then release and join the cancellation drainer, so no handleBatch
	// runs after batchLoop returns and touches resources the caller tears down on
	// shutdown. No new timer can be armed past this point.
	mu.Lock()
	if flushTimer != nil {
		if flushTimer.Stop() {
			timerWG.Done()
		}
		flushTimer = nil
	}
	mu.Unlock()
	timerWG.Wait()

	// On a cancelled shutdown the drainer already emptied the buffer while the
	// iterator was alive, so closing stopDrainer is a no-op it never observes;
	// joining it guarantees that drain completed. We deliberately do NOT flush
	// here: on any non-cancellation return (e.g. a stream error) the iterator is
	// torn down so ack/nack are no-ops, and flushing would only re-run handlers
	// against messages pubsub will redeliver anyway.
	close(stopDrainer)
	<-drainerDone

	if err != nil {
		return fmt.Errorf("receive batch: %w", err)
	}

	return nil
}

func (s *psSubscriber[M]) handleBatch(ctx context.Context, batch []incomingMessage, f func(context.Context, []M, []MessageMetadata) error) {
	if len(batch) == 0 {
		return
	}

	msgs := make([]M, 0, len(batch))
	metas := make([]MessageMetadata, 0, len(batch))

	// owned reports the messages this batch is responsible for acking/nacking.
	// While every message decodes that is the whole batch, so we avoid copying it
	// into a separate slice; once a poison message is dropped (and nacked
	// individually) we switch to valid, which holds only the decoded messages so
	// the dropped ones are not acked back into existence.
	var valid []incomingMessage
	dropped := false
	owned := func() []incomingMessage {
		if dropped {
			return valid
		}
		return batch
	}

	ackAll := func() {
		for _, m := range owned() {
			m.ack()
		}
	}
	nackAll := func() {
		for _, m := range owned() {
			m.nack()
		}
	}

	// Registered first so it runs last: on the happy path it acks the batch, and
	// after a nack it is a no-op since pubsub honours the first ack/nack per
	// message.
	defer ackAll()

	// Registered before the unmarshal loop so a panic anywhere below (in s.new,
	// proto.Unmarshal, or the handler) is recovered and nacks the batch for
	// redelivery instead of crashing the receive goroutine, mirroring handle.
	defer func() {
		if r := recover(); r != nil {
			s.logger.ErrorContext(ctx, "panic recovered while processing pubsub message batch",
				attr.SlogErrorMessage(fmt.Sprintf("%v", r)),
				attr.SlogErrorStack(string(debug.Stack())),
				attr.SlogTopicProtoName(s.topicProtoName),
				attr.SlogSubscriptionProtoName(s.subscriptionProtoName),
				attr.SlogSubscriberMessageID(batch[0].id),
				attr.SlogSubscriberBatchSize(len(batch)),
			)
			nackAll()
		}
	}()

	for i, m := range batch {
		msg := s.new()
		if err := proto.Unmarshal(m.data, msg); err != nil {
			s.logger.ErrorContext(
				ctx,
				"failed to unmarshal pubsub message",
				attr.SlogError(err),
				attr.SlogTopicProtoName(s.topicProtoName),
				attr.SlogSubscriptionProtoName(s.subscriptionProtoName),
				attr.SlogSubscriberMessageID(m.id),
				attr.SlogSubscriberDeliveryAttempt(m.deliveryAttempt),
			)
			// First drop: seed valid with the messages decoded so far (all of
			// batch[:i], since a prior drop would have set dropped already).
			if !dropped {
				valid = append(valid, batch[:i]...)
				dropped = true
			}
			m.nack()
			continue
		}
		msgs = append(msgs, msg)
		metas = append(metas, MessageMetadata{
			ID:              m.id,
			Attributes:      m.attributes,
			DeliveryAttempt: m.deliveryAttempt,
		})
		if dropped {
			valid = append(valid, m)
		}
	}

	if len(msgs) == 0 {
		return
	}

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
