package storage_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/pstest"
	"github.com/parquet-go/parquet-go"
	"github.com/speakeasy-api/gram/infra/internal/storagefixture"
	v1 "github.com/speakeasy-api/gram/infra/internal/storagefixture/pb/fixture/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/infra/pkg/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type captureStore struct {
	mu     sync.Mutex
	data   []byte
	object storage.Object
	cancel context.CancelFunc
}

func (s *captureStore) Write(_ context.Context, object storage.Object, encode func(io.Writer) error) error {
	var out bytes.Buffer
	if err := encode(&out); err != nil {
		return err
	}
	s.mu.Lock()
	s.data, s.object = out.Bytes(), object
	s.mu.Unlock()
	s.cancel()
	return nil
}

func TestRun_GeneratedBindingThroughPubSub(t *testing.T) {
	t.Parallel()
	server := pstest.NewServer()
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	conn, err := grpc.NewClient(server.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	client, err := pubsub.NewClient(t.Context(), "test-project", option.WithGRPCConn(conn))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	broker := gcp.NewEmulatedPubSub(slog.New(slog.DiscardHandler), "test-project", client, nil)
	def := storagefixture.FixtureV1Archive()
	// Reconciliation precedes publication; Run resolves the same resources.
	_, err = broker.StorageSubscriberForMessage(t.Context(), def.Payload, def.Marker)
	require.NoError(t, err)
	publisher, err := gcp.PubSubPublisherForMessage(t.Context(), broker, &v1.Event{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, publisher.Stop(context.WithoutCancel(t.Context()))) })
	_, err = publisher.Publish(t.Context(), v1.Event_builder{Id: new("hello")}.Build()).Get(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store := &captureStore{cancel: cancel}
	require.NoError(t, storage.Run(ctx, def, storage.Config{Broker: broker, Store: store, Buckets: map[string]string{def.Bucket: "123-fixture-archive"}, Settings: storage.Settings{MaxMessages: 1}}))
	store.mu.Lock()
	defer store.mu.Unlock()
	require.NotEmpty(t, store.data)
	file, err := parquet.OpenFile(bytes.NewReader(store.data), int64(len(store.data)))
	require.NoError(t, err)
	require.Equal(t, int64(1), file.NumRows())
	require.Contains(t, store.object.Name, "fixture.v1.Archive/part__year=")
}
