package gcp

import (
	"context"
	"errors"
	"testing"

	"cloud.google.com/go/pubsub/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	pingv2 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"
)

var errNoTopicOption = errors.New("declares no pubsub topic")

// stubBroker hands back a publisher without contacting Pub/Sub, and counts how
// often it was asked so caching can be observed.
type stubBroker struct {
	calls int
	err   error
}

func (s *stubBroker) PublisherForMessage(ctx context.Context, msg proto.Message) (*pubsub.Publisher, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}

	// Unauthenticated and pointed at nothing: gRPC dials lazily, so no
	// connection is attempted and no credentials are needed.
	client, err := pubsub.NewClient(ctx, "test-project",
		option.WithoutAuthentication(),
		option.WithEndpoint("localhost:1"),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	if err != nil {
		return nil, err
	}

	return client.Publisher("test-topic"), nil
}

func resolveOnly(msgs ...proto.Message) TopicResolver {
	registry := map[protoreflect.FullName]proto.Message{}
	for _, msg := range msgs {
		registry[proto.MessageName(msg)] = msg
	}

	return func(name protoreflect.FullName) (proto.Message, bool) {
		msg, ok := registry[name]
		return msg, ok
	}
}

func TestRawPublisher_UnknownTopicIsPermanent(t *testing.T) {
	t.Parallel()

	broker := &stubBroker{}
	pub := NewRawPublisher(broker, resolveOnly())

	_, err := pub.PublishRaw(t.Context(), "gram.nope.v1.Missing", []byte("x"), nil).Get(t.Context())

	require.ErrorIs(t, err, ErrUnknownTopic,
		"an unregistered topic must be distinguishable so callers dead-letter instead of retrying forever")
	require.Zero(t, broker.calls, "resolution must fail before the broker is consulted")
}

func TestRawPublisher_BrokerFailureIsPermanent(t *testing.T) {
	t.Parallel()

	broker := &stubBroker{err: errNoTopicOption}
	pub := NewRawPublisher(broker, resolveOnly(&pingv2.Message{}))

	_, err := pub.PublishRaw(t.Context(), proto.MessageName(&pingv2.Message{}), []byte("x"), nil).Get(t.Context())

	require.ErrorIs(t, err, ErrUnknownTopic,
		"a message that declares no topic is as unpublishable as one that does not exist")
	require.ErrorIs(t, err, errNoTopicOption, "the underlying cause must stay inspectable")
}

func TestRawPublisher_CachesPublisherPerTopic(t *testing.T) {
	t.Parallel()

	broker := &stubBroker{}
	pub := NewRawPublisher(broker, resolveOnly(&pingv2.Message{}))

	name := proto.MessageName(&pingv2.Message{})
	for range 3 {
		pub.PublishRaw(t.Context(), name, []byte("x"), nil)
	}

	require.Equal(t, 1, broker.calls, "each topic should open exactly one publisher regardless of message volume")
}

// TestRawMessageAttributes_CallerAttributesSurvive pins the behaviour the whole
// attributes column exists for. The relay republishes a message long after it
// was written, under a trace of its own; if the derived markers were allowed to
// win wholesale, or if trace context were injected here, the link from producer
// to subscriber would be silently lost.
func TestRawMessageAttributes_CallerAttributesSurvive(t *testing.T) {
	t.Parallel()

	attrs := rawMessageAttributes(map[string]string{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"event_type":  "audit_log.asset_event_v1",
	}, &pingv2.Message{})

	require.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", attrs["traceparent"])
	require.Equal(t, "audit_log.asset_event_v1", attrs["event_type"])
}

func TestRawMessageAttributes_DerivedMarkersCannotBeOverridden(t *testing.T) {
	t.Parallel()

	attrs := rawMessageAttributes(map[string]string{
		"content-type": "application/json",
		"schema":       "gram.lies.v1.NotThis",
	}, &pingv2.Message{})

	require.Equal(t, "application/x-protobuf", attrs["content-type"],
		"a stored attribute map must not be able to misdeclare the wire format of its own payload")
	require.Equal(t, string(proto.MessageName(&pingv2.Message{})), attrs["schema"])
}

func TestRawPublisher_StopIsSafeWithoutPublishers(t *testing.T) {
	t.Parallel()

	pub := NewRawPublisher(&stubBroker{}, resolveOnly())
	require.NoError(t, pub.Stop(context.Background()))
}
