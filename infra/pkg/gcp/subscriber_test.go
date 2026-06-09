package gcp

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// captureMessage records ack/nack calls made against an incomingMessage.
type captureMessage struct {
	mu      sync.Mutex
	acked   bool
	nacked  bool
	ackHits int
}

func (c *captureMessage) onAck() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acked = true
	c.ackHits++
}

func (c *captureMessage) onNack() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nacked = true
}

func newPanicSubscriber(logger *slog.Logger) *psSubscriber[*emptypb.Empty] {
	return &psSubscriber[*emptypb.Empty]{
		sub:                   nil,
		new:                   func() *emptypb.Empty { return &emptypb.Empty{} },
		logger:                logger,
		topicProtoName:        "test.v1.TopicMessage",
		subscriptionProtoName: "test.v1.SubscriptionMarker",
	}
}

func TestHandle_PanicIsLoggedAndNacked(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))

	s := newPanicSubscriber(logger)

	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	attempt := 3
	m := incomingMessage{
		id:              "msg-123",
		data:            nil,
		attributes:      nil,
		deliveryAttempt: &attempt,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}

	s.handle(t.Context(), m, func(context.Context, *emptypb.Empty, MessageMetadata) error {
		panic("boom")
	})

	require.True(t, cap.nacked, "message should be nacked after a panic")
	require.True(t, cap.acked, "deferred ack still runs; pubsub first-call-wins makes it a no-op after nack")

	logged := buf.String()
	require.Contains(t, logged, "panic recovered while processing pubsub message")
	require.Contains(t, logged, "boom")
	require.Contains(t, logged, "msg-123")
	require.Contains(t, logged, "delivery_attempt=3")
	require.Contains(t, logged, "stack=")
	require.Contains(t, logged, "test.v1.TopicMessage")
	require.Contains(t, logged, "test.v1.SubscriptionMarker")
}

func TestHandle_UnmarshalErrorIsNacked(t *testing.T) {
	t.Parallel()

	s := newPanicSubscriber(slog.New(slog.DiscardHandler))

	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	called := false
	m := incomingMessage{
		id:              "msg-bad",
		data:            []byte("not-a-valid-proto-wire-format-\xff\xff"),
		attributes:      nil,
		deliveryAttempt: nil,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}

	s.handle(t.Context(), m, func(context.Context, *emptypb.Empty, MessageMetadata) error {
		called = true
		return nil
	})

	require.True(t, cap.nacked, "message should be nacked when it fails to unmarshal")
	require.False(t, called, "handler should not run when unmarshalling fails")
}

func TestHandle_SuccessIsAckedOnly(t *testing.T) {
	t.Parallel()

	s := newPanicSubscriber(slog.New(slog.DiscardHandler))

	data, err := proto.Marshal(&emptypb.Empty{})
	require.NoError(t, err)

	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	var gotMeta MessageMetadata
	m := incomingMessage{
		id:              "msg-ok",
		data:            data,
		attributes:      map[string]string{"k": "v"},
		deliveryAttempt: nil,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}

	s.handle(t.Context(), m, func(_ context.Context, _ *emptypb.Empty, meta MessageMetadata) error {
		gotMeta = meta
		return nil
	})

	require.True(t, cap.acked, "message should be acked on success")
	require.False(t, cap.nacked, "message should not be nacked on success")
	require.Equal(t, "msg-ok", gotMeta.ID)
	require.Equal(t, map[string]string{"k": "v"}, gotMeta.Attributes)
}

func TestHandle_DeliveryAttemptOmittedWhenNil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	}))

	s := newPanicSubscriber(logger)

	cap := &captureMessage{} //nolint:exhaustruct // zero values are the initial state under test
	m := incomingMessage{
		id:              "msg-nil-attempt",
		data:            nil,
		attributes:      nil,
		deliveryAttempt: nil,
		ack:             cap.onAck,
		nack:            cap.onNack,
	}

	s.handle(t.Context(), m, func(context.Context, *emptypb.Empty, MessageMetadata) error {
		panic("kaboom")
	})

	require.True(t, cap.nacked)
	require.True(t, strings.Contains(buf.String(), "delivery_attempt=0"), "nil delivery attempt should log as 0")
}
