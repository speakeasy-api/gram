package roleprovisioning_test

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"cloud.google.com/go/pubsub/v2/pstest"
	"github.com/google/uuid"
	pluginsv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

// Only broker discovery is stubbed; decoding, batch result association and
// transport acknowledgement all use the production subscriber implementation.
type maintenanceBatchBroker struct {
	subscriber *pubsub.Subscriber
}

func (b maintenanceBatchBroker) SubscriberForMessage(context.Context, proto.Message, proto.Message) (*pubsub.Subscriber, error) {
	return b.subscriber, nil
}

func TestMaintenanceReceiveBatchMalformedNacksMissingPluginAcks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	server := pstest.NewServer()
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	conn, err := grpc.NewClient(server.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	// The Pub/Sub client also closes this connection; keep fallback cleanup for
	// client construction failures without requiring a second close to succeed.
	t.Cleanup(func() { _ = conn.Close() })
	client, err := pubsub.NewClient(ctx, "maintenance-test", option.WithGRPCConn(conn))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	// Protobuf request literals deliberately set only the relevant emulator fields.
	topic, err := server.GServer.CreateTopic(ctx, &pubsubpb.Topic{Name: "projects/maintenance-test/topics/roles"})
	require.NoError(t, err)
	subscription, err := server.GServer.CreateSubscription(ctx, &pubsubpb.Subscription{Name: "projects/maintenance-test/subscriptions/roles", Topic: topic.GetName(), AckDeadlineSeconds: 60})
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	subscriber, err := gcp.PubSubSubscriberForMessage(ctx, maintenanceBatchBroker{subscriber: client.Subscriber(subscription.GetName())}, new(pluginsv1.RoleProvisioningRequested), new(pluginsv1.RoleProvisioningConsumer), gcp.WithSubscriberLogger(logger))
	require.NoError(t, err)
	malformed := maintenanceRequest(f.org)
	malformed.SetPluginId("malformed-private-value")
	missing := maintenanceRequest(f.org)
	missing.SetPluginId(uuid.NewString())
	malformedData, err := proto.Marshal(malformed)
	require.NoError(t, err)
	missingData, err := proto.Marshal(missing)
	require.NoError(t, err)
	malformedID := server.Publish(topic.GetName(), malformedData, nil)
	missingID := server.Publish(topic.GetName(), missingData, nil)
	consumer := roleprovisioning.NewConsumer(logger, f.db, f.service)
	batches := make(chan []string, 1)
	done := make(chan error, 1)
	go func() {
		done <- subscriber.ReceiveBatchWithResult(ctx, gcp.BatchReceiveSettings{MaxMessages: 2, MaxBytes: 0, MaxLatency: 10 * time.Second}, func(ctx context.Context, messages []streams.BatchMessage[*pluginsv1.RoleProvisioningRequested]) error {
			ids := make([]string, 0, len(messages))
			for _, message := range messages {
				ids = append(ids, message.Metadata.ID)
			}
			select {
			case batches <- ids:
			default:
			}
			return consumer.HandleBatchWithResult(ctx, messages)
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("subscriber did not stop")
		}
	})
	select {
	case ids := <-batches:
		require.ElementsMatch(t, []string{malformedID, missingID}, ids, "both outcomes must originate in the same receive batch")
	case <-ctx.Done():
		t.Fatal("receive batch timed out")
	}
	require.Eventually(t, func() bool {
		bad, good := server.Message(malformedID), server.Message(missingID)
		if bad.Acks != 0 || good.Acks == 0 {
			return false
		}
		for _, modack := range bad.Modacks {
			if modack.AckDeadline == 0 {
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond, "malformed hint must nack while the valid missing-resource hint acks")
	require.Empty(t, maintenanceMessages(t, f), "neither message should create fresh maintenance work")
}
