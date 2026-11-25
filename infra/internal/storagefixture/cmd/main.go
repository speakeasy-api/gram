// A self-contained installation example using an in-process Pub/Sub test broker.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/pstest"
	"github.com/speakeasy-api/gram/infra/internal/storagefixture"
	v1 "github.com/speakeasy-api/gram/infra/internal/storagefixture/pb/fixture/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/infra/pkg/storage"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	out := flag.String("out", "", "Directory for the committed example Parquet file")
	flag.Parse()

	if err := run(context.Background(), *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, out string) error {
	if out == "" {
		return fmt.Errorf("--out is required")
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	server := pstest.NewServer()
	defer server.Close()

	conn, err := grpc.NewClient(server.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect test broker: %w", err)
	}

	client, err := pubsub.NewClient(ctx, "storage-demo", option.WithGRPCConn(conn))
	if err != nil {
		return fmt.Errorf("create pubsub client: %w", err)
	}
	defer client.Close()

	broker := gcp.NewEmulatedPubSub(slog.New(slog.DiscardHandler), "storage-demo", client, nil)
	def := storagefixture.FixtureV1Archive()
	if _, err := broker.StorageSubscriberForMessage(ctx, def.Payload, def.Marker); err != nil {
		return err
	}

	publisher, err := gcp.PubSubPublisherForMessage(ctx, broker, &v1.Event{})
	if err != nil {
		return err
	}
	defer publisher.Stop(context.WithoutCancel(ctx))

	if _, err := publisher.Publish(ctx, v1.Event_builder{Id: new("example"), Tags: []string{"demo", "parquet"}, Number: new(int32(0))}.Build()).Get(ctx); err != nil {
		return err
	}

	store := &localStore{directory: out, cancel: cancel}
	if err := storage.Run(ctx, def, storage.Config{Broker: broker, Store: store, Buckets: map[string]string{def.Bucket: "local-demo-bucket"}, Settings: storage.Settings{MaxMessages: 1}}); err != nil {
		return err
	}
	if store.committed == "" {
		return fmt.Errorf("example did not commit before its deadline")
	}

	fmt.Println(store.committed)

	return nil
}

// localStore implements only object durability; it is not a message handler.
type localStore struct {
	directory string
	committed string
	cancel    context.CancelFunc
}

func (s *localStore) Write(ctx context.Context, object storage.Object, encode func(io.Writer) error) error {
	path := filepath.Join(s.directory, filepath.FromSlash(object.Name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	f, err := os.CreateTemp(filepath.Dir(path), ".parquet-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()

	if err := encode(f); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()

	if err := commitLocalFile(f.Name(), path, dir.Sync); err != nil {
		return err
	}

	s.committed = path
	s.cancel()

	return nil
}

// commitLocalFile publishes without overwriting and rolls back the link if the
// directory cannot be synced. syncDir is injectable for filesystem-failure tests.
func commitLocalFile(temporary, final string, syncDir func() error) error {
	if err := os.Link(temporary, final); err != nil {
		return err
	}

	if err := syncDir(); err != nil {
		if removeErr := os.Remove(final); removeErr != nil {
			return errors.Join(err, fmt.Errorf("remove uncommitted local object: %w", removeErr))
		}

		return err
	}

	return nil
}
