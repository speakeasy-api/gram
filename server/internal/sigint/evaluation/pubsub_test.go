package evaluation

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/infra/gen"
	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestPubSubEvaluationPublishesCompleteSuccess(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, os.Getenv("PUBSUB_EMULATOR_HOST"), "requires Pub/Sub emulator")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	project := "sigint-test-" + uuid.NewString()
	client, err := pubsub.NewClient(ctx, project)
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	broker := gcp.NewEmulatedPubSub(testenv.NewLogger(t), project, client, gen.Descriptors)
	inputPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &conversationv1.Message{})
	require.NoError(t, err)
	defer func() { require.NoError(t, inputPub.Stop(context.Background())) }()
	outputPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &sigintv1.Reading{})
	require.NoError(t, err)
	defer func() { require.NoError(t, outputPub.Stop(context.Background())) }()
	sub, err := gcp.PubSubSubscriberForMessage(ctx, broker, &conversationv1.Message{}, &sigintv1.Evaluator{})
	require.NoError(t, err)
	// A test-only sink receives readings without adding a production storage consumer.
	var sink pubsubpb.Subscription
	sink.Name = "projects/" + project + "/subscriptions/readings-test"
	sink.Topic = "projects/" + project + "/topics/gram-sigint-v1-reading"
	_, err = client.SubscriptionAdminClient.CreateSubscription(ctx, &sink)
	require.NoError(t, err)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, name := range []string{"readings-test", "gram-sigint-v1-evaluator"} {
			require.NoError(t, client.SubscriptionAdminClient.DeleteSubscription(cleanup, &pubsubpb.DeleteSubscriptionRequest{Subscription: "projects/" + project + "/subscriptions/" + name}))
		}
		for _, name := range []string{"gram-sigint-v1-reading", "gram-conversation-v1-message"} {
			require.NoError(t, client.TopicAdminClient.DeleteTopic(cleanup, &pubsubpb.DeleteTopicRequest{Topic: "projects/" + project + "/topics/" + name}))
		}
	}()
	m := message()
	c := classifiertest.NewMock(t)
	c.On("Classify", mock.Anything, mock.Anything).Return(partialResult(false))
	h, _ := handler(t, m, partialSensors(), c, testenv.NewMeterProvider(t))
	h.publisher = outputPub
	done := make(chan error, 1)
	go func() {
		done <- sub.ReceiveBatchWithResult(ctx, gcp.BatchReceiveSettings{MaxMessages: 1, MaxBytes: 1 << 20, MaxLatency: time.Millisecond}, h.HandleBatchWithResult)
	}()
	defer func() { cancel(); require.NoError(t, <-done) }()
	_, err = inputPub.Publish(ctx, m).Get(ctx)
	require.NoError(t, err)
	var request pubsubpb.PullRequest
	request.Subscription = sink.Name
	request.MaxMessages = 1
	var received *pubsubpb.PullResponse
	for ctx.Err() == nil {
		received, err = client.SubscriptionAdminClient.Pull(ctx, &request)
		require.NoError(t, err)
		if len(received.GetReceivedMessages()) > 0 {
			break
		}
	}
	require.NotEmpty(t, received.GetReceivedMessages())
	var reading sigintv1.Reading
	require.NoError(t, proto.Unmarshal(received.GetReceivedMessages()[0].GetMessage().GetData(), &reading))
	require.Equal(t, m.GetId(), reading.GetMessageId())
	require.Equal(t, "other", reading.GetSensorId())
	require.Len(t, reading.GetMultiLabel().GetSignals(), 1)
}
