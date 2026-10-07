package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/parquet-go/parquet-go"
	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type storeFunc func(context.Context, Object, func(io.Writer) error) error

func (f storeFunc) Write(ctx context.Context, o Object, encode func(io.Writer) error) error {
	return f(ctx, o, encode)
}

// testDefinition isolates settlement from mapping semantics, which are verified
// independently against generated files in storagefixture's DuckDB tests.
func testDefinition(t *testing.T, external bool) Definition {
	t.Helper()
	opts := pubsubv1.StorageSubscriptionOptions_builder{Topic: new("example.v1.Event"), Bucket: new("event-archive"), Name: new("overridden-subscription")}.Build()
	mode := pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_DAILY
	if external {
		mode = pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL
		opts.SetPartitioning(mode)
		opts.SetPartitionAttribute("partition")
		opts.SetPartitionKeys([]string{"region", "account"})
	}
	marker, payload := &descriptorpb.MessageOptions{}, &descriptorpb.MessageOptions{}
	proto.SetExtension(marker, pubsubv1.E_StorageSubscription, opts)
	proto.SetExtension(payload, pubsubv1.E_Topic, pubsubv1.TopicOptions_builder{}.Build())
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: new("example/v1/test.proto"), Package: new("example.v1"), Syntax: new("proto3"), Dependency: []string{"gcp/pubsub/v1/options.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{Name: new("Event"), Options: payload}, {Name: new("Archive"), Options: marker}},
	}, protoregistry.GlobalFiles)
	require.NoError(t, err)
	return Definition{Marker: dynamicpb.NewMessage(file.Messages().Get(1)), Payload: dynamicpb.NewMessage(file.Messages().Get(0)), ProtoName: "example.v1.Archive", SubscriptionID: "overridden-subscription", TopicID: "example-v1-event", Bucket: "event-archive", Partitioning: mode, PartitionAttribute: opts.GetPartitionAttribute(), PartitionKeys: opts.GetPartitionKeys(), Schema: parquet.NewSchema("Event", parquet.Group{"data": parquet.String()}), Fingerprint: strings.Repeat("a", 64), Decode: func(data []byte, _ Metadata) (parquet.Row, error) {
		if string(data) == "bad" {
			return nil, errors.New("bad protobuf")
		}
		return parquet.Row{parquet.ByteArrayValue(data).Level(0, 0, 0)}, nil
	}}
}

func testRunner(t *testing.T, store Store, external bool, settings Settings) (*runner, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, mp.Shutdown(context.WithoutCancel(t.Context()))) })
	r, err := newRunner(testDefinition(t, external), Config{Store: store, Buckets: map[string]string{"event-archive": "123-event-archive"}, Settings: settings, Logger: slog.New(slog.DiscardHandler), MeterProvider: mp})
	require.NoError(t, err)
	return r, reader
}

type settlement struct{ acks, nacks atomic.Int32 }

func testDelivery(data, route string, received time.Time) (*delivery, *settlement) {
	s := &settlement{}
	return &delivery{data: []byte(data), id: "message-id", partition: route, received: received, ack: func() { s.acks.Add(1) }, nack: func() { s.nacks.Add(1) }}, s
}

func TestProcess_AckAfterDurableCommitAndFooter(t *testing.T) {
	t.Parallel()
	m, settled := testDelivery("payload", "part__year=2026/part__month=10/part__day=07", time.Now())
	r, _ := testRunner(t, storeFunc(func(ctx context.Context, o Object, encode func(io.Writer) error) error {
		require.Equal(t, "123-event-archive", o.Bucket)
		require.True(t, strings.HasPrefix(o.Name, "example.v1.Archive/part__year=2026/part__month=10/part__day=07/"))
		require.Equal(t, MappingVersion, o.Metadata["mapping_version"])
		var out bytes.Buffer
		require.NoError(t, encode(&out))
		file, err := parquet.OpenFile(bytes.NewReader(out.Bytes()), int64(out.Len()))
		require.NoError(t, err)
		require.Equal(t, int64(1), file.NumRows())
		require.Zero(t, settled.acks.Load(), "encoding and writing the footer alone do not acknowledge")
		return nil
	}), false, Settings{})
	r.process(t.Context(), []*delivery{m})
	require.Equal(t, int32(1), settled.acks.Load())
	require.Zero(t, settled.nacks.Load())
}

func TestProcess_FailedPartitionPreservesSuccessfulAcks(t *testing.T) {
	t.Parallel()
	committed := make(chan struct{})
	ok, okState := testDelivery("ok", "region=ok", time.Now())
	bad, badState := testDelivery("failure", "region=fail", time.Now())
	r, _ := testRunner(t, storeFunc(func(ctx context.Context, o Object, encode func(io.Writer) error) error {
		if strings.Contains(o.Name, "region=fail") {
			<-committed
			return errors.New("upload failed")
		}
		if err := encode(io.Discard); err != nil {
			return err
		}
		close(committed)
		return nil
	}), false, Settings{})
	r.process(t.Context(), []*delivery{ok, bad})
	require.Equal(t, int32(1), okState.acks.Load())
	require.Zero(t, okState.nacks.Load())
	require.Equal(t, int32(1), badState.nacks.Load())
	require.Zero(t, badState.acks.Load())
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestProcess_FailuresAndPanicsNeverAck(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"before-encode", "footer", "ambiguous-commit", "panic"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			m, state := testDelivery("payload", "region=one", time.Now())
			r, _ := testRunner(t, storeFunc(func(ctx context.Context, o Object, encode func(io.Writer) error) error {
				switch failure {
				case "before-encode":
					return errors.New("unavailable")
				case "footer":
					return encode(failingWriter{})
				case "panic":
					panic("encoder crashed")
				default:
					if err := encode(io.Discard); err != nil {
						return err
					}
					return errors.New("object may have committed but close response was lost")
				}
			}), false, Settings{})
			r.process(t.Context(), []*delivery{m})
			require.Zero(t, state.acks.Load())
			require.Equal(t, int32(1), state.nacks.Load())
		})
	}
}

func TestProcess_CancellationAfterCommitStillAcks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m, state := testDelivery("payload", "region=one", time.Now())
	r, _ := testRunner(t, storeFunc(func(ctx context.Context, o Object, encode func(io.Writer) error) error {
		if err := encode(io.Discard); err != nil {
			return err
		}
		cancel()
		return nil
	}), false, Settings{})
	r.process(ctx, []*delivery{m})
	require.Equal(t, int32(1), state.acks.Load())
	require.Zero(t, state.nacks.Load())
}

func TestProcess_DeadlineAndShutdownNackUncommitted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, state := testDelivery("payload", "region=one", time.Now())
		r, _ := testRunner(t, storeFunc(func(ctx context.Context, o Object, encode func(io.Writer) error) error {
			<-ctx.Done()
			return ctx.Err()
		}), false, Settings{ProcessTimeout: time.Second})
		r.process(t.Context(), []*delivery{m})
		require.Zero(t, state.acks.Load())
		require.Equal(t, int32(1), state.nacks.Load())
	})
}

func TestProcess_PoisonRowsDoNotDiscardGoodRows(t *testing.T) {
	t.Parallel()
	good, goodState := testDelivery("ok", "region=one", time.Now())
	bad, badState := testDelivery("bad", "region=one", time.Now())
	r, _ := testRunner(t, storeFunc(func(ctx context.Context, o Object, encode func(io.Writer) error) error { return encode(io.Discard) }), false, Settings{})
	r.process(t.Context(), []*delivery{bad, good})
	require.Equal(t, int32(1), goodState.acks.Load())
	require.Equal(t, int32(1), badState.nacks.Load())
	require.Zero(t, badState.acks.Load())
}

func TestProcess_OnlyPoisonSkipsObjectCreation(t *testing.T) {
	t.Parallel()
	m, state := testDelivery("bad", "region=one", time.Now())
	var calls atomic.Int32
	r, _ := testRunner(t, storeFunc(func(context.Context, Object, func(io.Writer) error) error { calls.Add(1); return nil }), false, Settings{})
	r.process(t.Context(), []*delivery{m})
	require.Zero(t, calls.Load())
	require.Equal(t, int32(1), state.nacks.Load())
}

func TestAccept_PermanentDropsHaveFiniteLabels(t *testing.T) {
	t.Parallel()
	r, reader := testRunner(t, storeFunc(func(context.Context, Object, func(io.Writer) error) error { return nil }), true, Settings{})
	for _, attributes := range []map[string]string{nil, {"partition": "region=../account=1"}, {"partition": strings.Repeat("x", 513)}} {
		m, state := testDelivery("payload", "", time.Now())
		r.accept(t.Context(), m, attributes, func(context.Context, *delivery) { t.Error("invalid partition entered batching") })
		require.Equal(t, int32(1), state.acks.Load())
		require.Zero(t, state.nacks.Load())
	}
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	var reasons []string
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "storage_subscription_dropped_messages" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				label, _ := point.Attributes.Value("proto_message")
				require.Equal(t, "example-v1-archive", label.AsString(), "transport name override must not change the metric label")
				reason, _ := point.Attributes.Value("reason")
				reasons = append(reasons, reason.AsString())
				require.Equal(t, int64(1), point.Value)
				require.Equal(t, 2, point.Attributes.Len())
			}
		}
	}
	require.ElementsMatch(t, []string{string(DropMissing), string(DropMalformed), string(DropLimit)}, reasons)
}

func TestNewRunner_RejectsStaleMappingAndLeaseBudgets(t *testing.T) {
	t.Parallel()
	def := testDefinition(t, false)
	config := Config{Store: storeFunc(func(context.Context, Object, func(io.Writer) error) error { return nil })}
	_, err := newRunner(def, config)
	require.ErrorContains(t, err, "bucket mapping")
	config.Buckets = map[string]string{def.Bucket: "123-event-archive"}
	config.Settings = Settings{ProcessTimeout: 5 * time.Minute}
	_, err = newRunner(def, config)
	require.ErrorContains(t, err, "lease must exceed")
	config.Settings = Settings{}
	def.TopicID = "wrong-topic"
	_, err = newRunner(def, config)
	require.ErrorContains(t, err, "topic definition is stale")
}
