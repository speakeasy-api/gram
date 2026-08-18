package topics

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pingv2 "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
)

// stubBroker returns a publisher without contacting Pub/Sub, and counts how
// often it was asked so lazy construction and caching can be observed.
type stubBroker struct {
	calls   int
	err     error
	clients []*pubsub.Client
}

// newStubBroker registers a cleanup that closes every client the broker
// created: Mux.Stop stops publishers but has no view of the clients behind
// them, so without this each test leaks a gRPC client and its goroutines.
func newStubBroker(t *testing.T) *stubBroker {
	t.Helper()

	broker := &stubBroker{calls: 0, err: nil, clients: nil}
	t.Cleanup(func() {
		for _, client := range broker.clients {
			_ = client.Close()
		}
	})

	return broker
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

	s.clients = append(s.clients, client)

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
	require.Contains(t, All(), GramWebhooksV1Event)

	for _, topic := range All() {
		_, ok := Lookup(string(topic))
		require.Truef(t, ok, "%s is listed by All but does not resolve", topic)
	}
}

func TestMuxPublish_UnknownTopic(t *testing.T) {
	t.Parallel()

	broker := newStubBroker(t)
	mux := NewMux(broker, nil)

	_, err := mux.Publish(t.Context(), "gram.nope.v1.Missing", []byte("x"), nil).Get(t.Context())

	require.ErrorIs(t, err, ErrUnknownTopic)
	require.Zero(t, broker.calls, "an undeclared topic must fail before a publisher is opened")
}

// TestMuxPublish_DoesNotDecodePayloads pins the byte-orientation of the whole
// package: bytes that do not parse as the topic's message type are still
// handed to Pub/Sub, because deciding whether they parse is the topic's
// server-side BINARY schema's job. A client-side decode here would re-render
// the payload, and the transactional outbox depends on the committed bytes
// reaching the wire verbatim.
func TestMuxPublish_DoesNotDecodePayloads(t *testing.T) {
	t.Parallel()

	broker := newStubBroker(t)
	mux := NewMux(broker, boundedSettings())
	t.Cleanup(func() { _ = mux.Stop(context.Background()) })

	// Field 1, length-delimited, with the length byte truncated.
	corrupt := []byte{0x0A, 0xFF}

	_, err := mux.Publish(t.Context(), string(GramPingV2Message), corrupt, nil).Get(t.Context())

	// The failure must come from the wire, not from a decode: a client-side
	// parse check would reject the corrupt bytes immediately, before the
	// unreachable endpoint had a chance to. A transport error is proof the
	// payload was handed over untouched.
	require.Error(t, err, "the unreachable endpoint must fail the publish")
	require.Truef(t,
		status.Code(err) == codes.Unavailable ||
			status.Code(err) == codes.DeadlineExceeded ||
			errors.Is(err, context.DeadlineExceeded),
		"expected a transport failure from the unreachable endpoint, not a client-side rejection: %v", err)
	require.Equal(t, 1, broker.calls, "the payload must reach the publisher without a decode standing in the way")
}

func TestMuxPublish_CachesPublisherPerTopic(t *testing.T) {
	t.Parallel()

	broker := newStubBroker(t)
	mux := NewMux(broker, boundedSettings())
	t.Cleanup(func() { _ = mux.Stop(context.Background()) })

	data, err := proto.Marshal(&pingv2.Message{})
	require.NoError(t, err)

	for range 3 {
		mux.Publish(t.Context(), string(GramPingV2Message), data, nil)
	}

	require.Equal(t, 1, broker.calls, "each topic should open exactly one publisher")
}

// TestMuxPublish_BrokerFailureIsRetryable is the distinction that keeps a
// valid row out of the dead letter table. Creating a publisher is not a pure
// lookup — the emulator reconciles the topic over the network first, and a
// cancelled context lands here too — so a failure at this point says nothing
// about whether the topic exists.
func TestMuxPublish_BrokerFailureIsRetryable(t *testing.T) {
	t.Parallel()

	broker := newStubBroker(t)
	broker.err = context.DeadlineExceeded
	mux := NewMux(broker, nil)

	_, err := mux.Publish(t.Context(), string(GramRiskV1Finding), nil, nil).Get(t.Context())

	require.ErrorIs(t, err, context.DeadlineExceeded, "the underlying cause must stay inspectable")
	require.NotErrorIs(t, err, ErrUnknownTopic,
		"the topic is declared; the broker just could not be reached")
}

// TestMuxWarm_BuildsEveryDeclaredTopic pins what Warm is for: one broker probe
// per declared topic at boot, cached so first use pays nothing further.
func TestMuxWarm_BuildsEveryDeclaredTopic(t *testing.T) {
	t.Parallel()

	broker := newStubBroker(t)
	mux := NewMux(broker, boundedSettings())
	t.Cleanup(func() { _ = mux.Stop(context.Background()) })

	require.NoError(t, mux.Warm(t.Context()))
	require.Equal(t, len(All()), broker.calls, "every declared topic must be probed")

	data, err := proto.Marshal(&pingv2.Message{})
	require.NoError(t, err)
	mux.Publish(t.Context(), string(GramPingV2Message), data, nil)
	require.Equal(t, len(All()), broker.calls, "publishing after Warm must reuse the warmed publisher")
}

func TestMuxWarm_SurfacesBrokerFailure(t *testing.T) {
	t.Parallel()

	broker := newStubBroker(t)
	broker.err = context.DeadlineExceeded
	mux := NewMux(broker, nil)

	err := mux.Warm(t.Context())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, string(All()[0]), "the failure must name the topic it happened on")
}

func TestNoopPublisher(t *testing.T) {
	t.Parallel()

	pub := NewNoopPublisher()

	_, err := pub.Publish(t.Context(), "anything", []byte("x"), nil).Get(t.Context())
	require.NoError(t, err)
	require.NoError(t, pub.Stop(t.Context()))
}

// TestMuxPublish_AfterStopFails pins the shutdown lifecycle: once Stop has
// begun, a racing publish must fail rather than repopulate the cleared cache
// with a publisher nothing ever flushes.
func TestMuxPublish_AfterStopFails(t *testing.T) {
	t.Parallel()

	broker := newStubBroker(t)
	mux := NewMux(broker, boundedSettings())
	require.NoError(t, mux.Stop(context.Background()))

	_, err := mux.Publish(t.Context(), string(GramPingV2Message), []byte("x"), nil).Get(t.Context())
	require.ErrorContains(t, err, "stopped")
	require.Zero(t, broker.calls, "a stopped mux must not open new publishers")
}

func TestMuxStopIsSafeWithoutPublishers(t *testing.T) {
	t.Parallel()

	require.NoError(t, NewMux(newStubBroker(t), nil).Stop(context.Background()))
}

var _ gcp.PublisherBroker = (*stubBroker)(nil)
