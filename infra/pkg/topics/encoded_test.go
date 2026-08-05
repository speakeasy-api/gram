package topics

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	pingv2 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

// stubBroker returns a publisher without contacting Pub/Sub, and counts how
// often it was asked so lazy construction and caching can be observed.
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

// boundedSettings keeps a publish that reaches the stub broker's unreachable
// endpoint from blocking on the client's default one-minute timeout.
func boundedSettings() *pubsub.PublishSettings {
	settings := pubsub.DefaultPublishSettings
	settings.Timeout = 100 * time.Millisecond
	return &settings
}

func TestLookup(t *testing.T) {
	t.Parallel()

	topic, ok := Lookup("gram.risk.v1.Finding")
	require.True(t, ok)
	require.Equal(t, GramRiskV1Finding, topic)

	_, ok = Lookup("gram.nope.v1.Missing")
	require.False(t, ok, "an undeclared name must not resolve")

	_, ok = Lookup("gram.ping.v2.Processor")
	require.False(t, ok, "a subscription marker declares no topic and must not resolve")
}

func TestAllCoversDeclaredTopics(t *testing.T) {
	t.Parallel()

	require.Contains(t, All(), GramPingV2Message)
	require.Contains(t, All(), GramRiskV1Finding)

	for _, topic := range All() {
		_, ok := Lookup(string(topic))
		require.Truef(t, ok, "%s is listed by All but does not resolve", topic)
	}
}

func TestSetPublish_UnknownTopic(t *testing.T) {
	t.Parallel()

	broker := &stubBroker{}
	set := NewSet(broker, nil)

	_, err := set.Publish(t.Context(), "gram.nope.v1.Missing", []byte("x")).Get(t.Context())

	require.ErrorIs(t, err, ErrUnknownTopic)
	require.Zero(t, broker.calls, "an undeclared topic must fail before a publisher is opened")
}

// TestSetPublish_PayloadMismatch covers the decode guard. Note what it does and
// does not prove: protobuf tolerates unknown fields, so a structurally valid
// payload from some other message type can still round-trip. What the generated
// binding guarantees is that whatever is published is an instance of the
// topic's declared type — corrupt or truncated bytes are rejected outright,
// including against the emulator, which performs no schema validation.
func TestSetPublish_PayloadMismatch(t *testing.T) {
	t.Parallel()

	set := NewSet(&stubBroker{}, nil)

	// Field 1, length-delimited, with the length byte truncated.
	corrupt := []byte{0x0A, 0xFF}

	_, err := set.Publish(t.Context(), string(GramRiskV1Finding), corrupt).Get(t.Context())

	require.ErrorIs(t, err, ErrPayloadMismatch)
}

func TestSetPublish_AcceptsMatchingPayload(t *testing.T) {
	t.Parallel()

	broker := &stubBroker{}
	set := NewSet(broker, boundedSettings())
	t.Cleanup(func() { _ = set.Stop(context.Background()) })

	requestID := "019fd155-dc78-7545-8901-c634e078da95"
	data, err := proto.Marshal(riskv1.Finding_builder{RequestId: &requestID}.Build())
	require.NoError(t, err)

	require.NotNil(t, set.Publish(t.Context(), string(GramRiskV1Finding), data))
	require.Equal(t, 1, broker.calls)
}

func TestSetPublish_CachesPublisherPerTopic(t *testing.T) {
	t.Parallel()

	broker := &stubBroker{}
	set := NewSet(broker, boundedSettings())
	t.Cleanup(func() { _ = set.Stop(context.Background()) })

	data, err := proto.Marshal(&pingv2.Message{})
	require.NoError(t, err)

	for range 3 {
		set.Publish(t.Context(), string(GramPingV2Message), data)
	}

	require.Equal(t, 1, broker.calls, "each topic should open exactly one publisher")
}

func TestSetPublish_BrokerFailurePropagates(t *testing.T) {
	t.Parallel()

	set := NewSet(&stubBroker{err: context.DeadlineExceeded}, nil)

	_, err := set.Publish(t.Context(), string(GramRiskV1Finding), nil).Get(t.Context())

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestNoopPublisher(t *testing.T) {
	t.Parallel()

	pub := NewNoopPublisher()

	_, err := pub.Publish(t.Context(), "anything", []byte("x")).Get(t.Context())
	require.NoError(t, err)
	require.NoError(t, pub.Stop(t.Context()))
}

func TestSetStopIsSafeWithoutPublishers(t *testing.T) {
	t.Parallel()

	require.NoError(t, NewSet(&stubBroker{}, nil).Stop(context.Background()))
}

var _ gcp.PublisherBroker = (*stubBroker)(nil)
