package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"github.com/ettle/strcase"
	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/speakeasy-api/gram/infra/internal/attr"
	"github.com/speakeasy-api/gram/infra/internal/batching"
	declarations "github.com/speakeasy-api/gram/infra/internal/gcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

const (
	// defaultMaxMessages keeps a normal batch below the unsettled count budget.
	defaultMaxMessages = 10_000

	// defaultMaxBytes triggers flushing at 32 MiB; queued batches can grow further.
	defaultMaxBytes = 32 << 20

	// defaultMaxLatency flushes quiet subscriptions within thirty seconds.
	defaultMaxLatency = 30 * time.Second

	// defaultMaxPartitions bounds object fanout from publisher-selected routes.
	defaultMaxPartitions = 128

	// defaultConcurrency bounds simultaneous encoders and GCS upload buffers.
	defaultConcurrency = 4

	// defaultProcessTimeout budgets all partitions in one batch, including queued work.
	defaultProcessTimeout = 2 * time.Minute

	// defaultMaxExtension leaves headroom for admission, queued batches and processing.
	defaultMaxExtension = 10 * time.Minute

	// defaultOutstandingMessages bounds admitted work until settlement.
	defaultOutstandingMessages = 20_000

	// defaultOutstandingBytes retains at most 128 MiB of admitted raw payloads.
	defaultOutstandingBytes = 128 << 20

	// parquetRowGroupRows bounds row-group work independently of input batches.
	parquetRowGroupRows = 256

	// parquetBufferBytes targets 64 KiB for in-memory page construction and output
	// buffers; encoded column pages are spooled to temporary files.
	parquetBufferBytes = 64 << 10
)

// Broker has a dedicated storage-owned path, implemented by both Go brokers.
type Broker interface {
	StorageSubscriberForMessage(context.Context, proto.Message, proto.Message) (*pubsub.Subscriber, error)
}

// Settings bounds input buffering and concurrent object processing. Zero fields
// use defaults; negative values are errors. Raw budgets exclude decoded values,
// Parquet page buffers and the Pub/Sub client's own outstanding callback budget.
type Settings struct {
	// MaxMessages defaults to 10,000 messages per batch.
	MaxMessages int

	// MaxBytes defaults to a 32 MiB payload flush threshold, not a batch size cap.
	// Queued batches may grow up to the count and outstanding byte limits.
	MaxBytes int

	// MaxLatency defaults to 30 seconds from the batch's first receipt.
	MaxLatency time.Duration

	// MaxPartitions defaults to 128 distinct routes per processing window.
	// Windows run sequentially within the same whole-batch processing deadline.
	MaxPartitions int

	// Concurrency defaults to four encode/upload workers within the active batch.
	Concurrency int

	// ProcessTimeout defaults to two minutes for the entire active batch.
	ProcessTimeout time.Duration

	// MaxExtension defaults to ten minutes of Pub/Sub lease extension.
	MaxExtension time.Duration

	// OutstandingMessages defaults to 20,000 retained deliveries.
	OutstandingMessages int

	// OutstandingBytes defaults to 128 MiB of retained raw input.
	OutstandingBytes int
}

// Config supplies process-owned dependencies for explicit runner installation.
type Config struct {
	// Broker resolves the declared topic and subscription.
	Broker Broker

	// Store durably commits immutable objects.
	Store Store

	// Buckets is the deployment's logical-to-physical mapping.
	Buckets map[string]string

	// Settings selects bounded batching and lease budgets.
	Settings Settings

	// TempDir is an existing directory for disk-backed Parquet column pages.
	// Empty uses os.TempDir(). Each object gets an isolated subdirectory removed
	// after encoding, including failures and cancellation. Use disk-backed
	// ephemeral storage; abrupt process termination can leave temporary files.
	TempDir string

	// MeterProvider defaults to the process's global OpenTelemetry provider.
	MeterProvider metric.MeterProvider

	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

type delivery struct {
	data      []byte
	id        string
	partition string
	received  time.Time
	ack       func()
	nack      func()
	once      sync.Once
}

func (m *delivery) settle(success bool) {
	m.once.Do(func() {
		if success {
			m.ack()
		} else {
			m.nack()
		}
	})
}

type runner struct {
	def           Definition
	config        Config
	settings      Settings
	bucket        string
	label         metric.MeasurementOption
	dropped       metric.Int64Counter
	objects       metric.Int64Counter
	written       metric.Int64Counter
	failures      metric.Int64Counter
	unsettled     metric.Int64UpDownCounter
	inputBytes    metric.Int64UpDownCounter
	writeDuration metric.Float64Histogram
	now           func() time.Time
}

// Run installs one generated storage subscription until ctx is cancelled.
// Call it from the process's errgroup; no application message handler is needed.
// Durable object commits are acknowledged independently, including if another
// partition fails or shutdown begins after an object has committed.
func Run(ctx context.Context, def Definition, config Config) error {
	r, err := newRunner(def, config)
	if err != nil {
		return err
	}
	if config.Broker == nil {
		return errors.New("storage broker is required")
	}

	sub, err := config.Broker.StorageSubscriberForMessage(ctx, def.Payload, def.Marker)
	if err != nil {
		return fmt.Errorf("resolve storage subscription: %w", err)
	}

	sub.ReceiveSettings.MaxOutstandingMessages = r.settings.OutstandingMessages
	sub.ReceiveSettings.MaxOutstandingBytes = r.settings.OutstandingBytes
	sub.ReceiveSettings.MaxExtension = r.settings.MaxExtension

	err = r.receive(ctx, func(ctx context.Context, deliver func(context.Context, *delivery)) error {
		return sub.Receive(ctx, func(callbackCtx context.Context, msg *pubsub.Message) {
			r.accept(callbackCtx, &delivery{data: msg.Data, id: msg.ID, received: r.now(), ack: msg.Ack, nack: msg.Nack}, msg.Attributes, deliver)
		})
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("receive storage subscription: %w", err)
	}

	return nil
}

func (r *runner) accept(ctx context.Context, m *delivery, attributes map[string]string, deliver func(context.Context, *delivery)) {
	route, reason := partition(r.def, attributes, m.received)
	if reason != DropNone {
		r.dropped.Add(ctx, 1, r.label, metric.WithAttributes(attr.StorageReason(reason)))
		m.settle(true)
		return
	}

	m.partition = route
	r.unsettled.Add(ctx, 1, r.label)
	r.inputBytes.Add(ctx, int64(len(m.data)), r.label)

	ack, nack := m.ack, m.nack
	release := func() {
		r.unsettled.Add(ctx, -1, r.label)
		r.inputBytes.Add(ctx, -int64(len(m.data)), r.label)
	}
	m.ack = func() { release(); ack() }
	m.nack = func() { release(); nack() }

	// Admission leaves room for encoding and settlement within this lease, even
	// when many SDK callbacks are waiting behind the application input budget.
	admitCtx, cancel := context.WithDeadline(ctx, m.received.Add(r.settings.MaxExtension-r.settings.ProcessTimeout-r.settings.MaxLatency))
	defer cancel()

	deliver(admitCtx, m)
}

func newRunner(def Definition, config Config) (*runner, error) {
	settings, err := defaultSettings(config.Settings)
	if err != nil {
		return nil, err
	}

	if config.Store == nil {
		return nil, errors.New("storage object store is required")
	}
	if def.Schema == nil || def.Decode == nil || def.Marker == nil || def.Payload == nil || len(def.Fingerprint) != 64 {
		return nil, errors.New("a generated storage definition is required")
	}

	md, pd := def.Marker.ProtoReflect().Descriptor(), def.Payload.ProtoReflect().Descriptor()
	opts, ok := declarations.StorageOptionsFromMessage(md)
	if !ok || string(md.FullName()) != def.ProtoName || strings.TrimSpace(opts.GetTopic()) != string(pd.FullName()) || opts.GetBucket() != def.Bucket || declarations.ResolveSubscriptionName(md, opts) != def.SubscriptionID {
		return nil, errors.New("storage definition disagrees with proto declaration; regenerate")
	}

	topic, ok := declarations.TopicOptionsFromMessage(pd)
	if !ok || strings.TrimSpace(topic.GetName()) != "" || declarations.ResolveTopicName(pd, topic) != def.TopicID {
		return nil, errors.New("storage topic definition is stale or has no attached schema; regenerate")
	}

	partitioning := opts.GetPartitioning()
	if partitioning == pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_UNSPECIFIED {
		partitioning = pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_DAILY
	}
	if partitioning != def.Partitioning || opts.GetPartitionAttribute() != def.PartitionAttribute || !slices.Equal(opts.GetPartitionKeys(), def.PartitionKeys) {
		return nil, errors.New("storage partition definition is stale; regenerate")
	}
	if partitioning != pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_DAILY && partitioning != pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_HOURLY && partitioning != pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL {
		return nil, errors.New("unsupported storage partitioning; regenerate")
	}
	if codec := opts.GetCodec(); codec != pubsubv1.StorageCodec_STORAGE_CODEC_UNSPECIFIED && codec != pubsubv1.StorageCodec_STORAGE_CODEC_PARQUET {
		return nil, errors.New("unsupported storage codec; regenerate")
	}

	bucket := config.Buckets[def.Bucket]
	if !validPhysicalBucket(bucket) {
		return nil, fmt.Errorf("missing or invalid physical bucket mapping for %q", def.Bucket)
	}

	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.MeterProvider == nil {
		config.MeterProvider = otel.GetMeterProvider()
	}

	meter := config.MeterProvider.Meter("github.com/speakeasy-api/gram/infra/pkg/storage")
	dropped, err := meter.Int64Counter("storage_subscription_dropped_messages", metric.WithUnit("{message}"))
	if err != nil {
		return nil, fmt.Errorf("create storage drops metric: %w", err)
	}

	objects, err := meter.Int64Counter("storage_subscription_uploaded_objects", metric.WithUnit("{object}"))
	if err != nil {
		return nil, fmt.Errorf("create storage objects metric: %w", err)
	}

	written, err := meter.Int64Counter("storage_subscription_written_messages", metric.WithUnit("{message}"))
	if err != nil {
		return nil, fmt.Errorf("create storage written metric: %w", err)
	}

	failures, err := meter.Int64Counter("storage_subscription_failures", metric.WithUnit("{failure}"))
	if err != nil {
		return nil, fmt.Errorf("create storage failure metric: %w", err)
	}

	unsettled, err := meter.Int64UpDownCounter("storage_subscription_unsettled_messages", metric.WithUnit("{message}"))
	if err != nil {
		return nil, fmt.Errorf("create storage input metric: %w", err)
	}

	inputBytes, err := meter.Int64UpDownCounter("storage_subscription_unsettled_bytes", metric.WithUnit("By"))
	if err != nil {
		return nil, fmt.Errorf("create storage input bytes metric: %w", err)
	}

	writeDuration, err := meter.Float64Histogram("storage_subscription_object_write_duration", metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create storage duration metric: %w", err)
	}

	return &runner{def: def, config: config, settings: settings, bucket: bucket, now: time.Now,
		label:   metric.WithAttributes(attr.StorageProtoMessage(strcase.ToKebab(def.ProtoName))),
		dropped: dropped, objects: objects, written: written, failures: failures,
		unsettled: unsettled, inputBytes: inputBytes, writeDuration: writeDuration,
	}, nil
}

// Physical names intentionally use the portable single-component GCS subset.
var physicalBucket = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

func defaultSettings(s Settings) (Settings, error) {
	if s.MaxMessages < 0 || s.MaxBytes < 0 || s.MaxLatency < 0 || s.MaxPartitions < 0 || s.Concurrency < 0 || s.ProcessTimeout < 0 || s.MaxExtension < 0 || s.OutstandingMessages < 0 || s.OutstandingBytes < 0 {
		return s, errors.New("storage settings cannot be negative")
	}

	if s.MaxMessages == 0 {
		s.MaxMessages = defaultMaxMessages
	}
	if s.MaxBytes == 0 {
		s.MaxBytes = defaultMaxBytes
	}
	if s.MaxLatency == 0 {
		s.MaxLatency = defaultMaxLatency
	}
	if s.MaxPartitions == 0 {
		s.MaxPartitions = defaultMaxPartitions
	}
	if s.Concurrency == 0 {
		s.Concurrency = defaultConcurrency
	}
	if s.ProcessTimeout == 0 {
		s.ProcessTimeout = defaultProcessTimeout
	}
	if s.MaxExtension == 0 {
		s.MaxExtension = defaultMaxExtension
	}
	if s.OutstandingMessages == 0 {
		s.OutstandingMessages = defaultOutstandingMessages
	}
	if s.OutstandingBytes == 0 {
		s.OutstandingBytes = defaultOutstandingBytes
	}

	// Require headroom for queueing, not a guarantee that the queue drains within
	// the lease. Each batch's deadline is also capped by its oldest delivery.
	if s.MaxExtension > time.Hour || s.ProcessTimeout >= (s.MaxExtension-s.MaxLatency)/2 {
		return s, errors.New("storage lease must exceed twice the batch processing timeout plus batch latency and be at most one hour")
	}

	return s, nil
}

func (r *runner) receive(ctx context.Context, receive func(context.Context, func(context.Context, *delivery)) error) error {
	return batching.Run(ctx, batching.Settings{
		MaxMessages: r.settings.MaxMessages, MaxBytes: r.settings.MaxBytes, MaxLatency: r.settings.MaxLatency,
		OutstandingMessages: r.settings.OutstandingMessages, OutstandingBytes: r.settings.OutstandingBytes,
	}, receive, func(m *delivery) int { return len(m.data) }, func(m *delivery) { m.settle(false) }, r.process)
}

func (r *runner) process(ctx context.Context, batch []*delivery) {
	defer func() {
		for _, m := range batch {
			m.settle(false)
		}
	}()

	deadline := r.now().Add(r.settings.ProcessTimeout)
	for _, m := range batch {
		if d := m.received.Add(r.settings.MaxExtension - r.settings.MaxLatency); d.Before(deadline) {
			deadline = d
		}
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	groups := map[string][]*delivery{}
	for _, m := range batch {
		if ctx.Err() != nil {
			return
		}

		if _, exists := groups[m.partition]; !exists && len(groups) == r.settings.MaxPartitions {
			r.writePartitions(ctx, groups)
			clear(groups)
		}
		groups[m.partition] = append(groups[m.partition], m)
	}

	r.writePartitions(ctx, groups)
}

// writePartitions bounds active writers within one partition window. All windows
// share their parent batch's deadline and settle successful objects independently.
func (r *runner) writePartitions(ctx context.Context, groups map[string][]*delivery) {
	var group errgroup.Group
	group.SetLimit(r.settings.Concurrency)
	for route, messages := range groups {
		group.Go(func() (err error) {
			started := r.now()
			defer func() {
				if recover() != nil {
					err = errors.New("panic encoding storage partition")
				}
				if err != nil {
					r.failures.Add(ctx, 1, r.label, metric.WithAttributes(attr.StorageReason("object_write")))
					r.config.Logger.ErrorContext(ctx, "write storage partition", attr.SlogError(err), attr.SlogSubscriptionProtoName(r.def.ProtoName))
				}
				r.writeDuration.Record(ctx, r.now().Sub(started).Seconds(), r.label)
			}()

			return r.writePartition(ctx, route, messages)
		})
	}

	// Failures are per-object outcomes, not terminal receive-loop errors.
	_ = group.Wait()
}

func (r *runner) writePartition(ctx context.Context, route string, messages []*delivery) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	decode := func(m *delivery) (parquet.Row, bool) {
		row, err := r.def.Decode(m.data, Metadata{MessageID: m.id, ReceivedMicros: m.received.UnixMicro()})
		if err != nil {
			m.settle(false)
			r.failures.Add(ctx, 1, r.label, metric.WithAttributes(attr.StorageReason("payload_decode")))
			return nil, false
		}

		return row, true
	}

	// Find a valid first row before opening a writer, avoiding empty objects for
	// poison-only partitions. Each subsequent row is decoded just before writing.
	var first parquet.Row
	start := 0
	for ; start < len(messages); start++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if row, ok := decode(messages[start]); ok {
			first = row
			break
		}
	}
	if first == nil {
		return nil
	}

	id, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("allocate object identity: %w", err)
	}

	object := Object{Bucket: r.bucket, Name: r.def.ProtoName + "/" + route + "/" + id.String() + ".parquet", Metadata: map[string]string{"schema_fingerprint": r.def.Fingerprint, "mapping_version": declarations.StorageMappingVersion, "subscription": r.def.ProtoName}}

	var written []*delivery
	err = r.config.Store.Write(ctx, object, func(out io.Writer) error {
		tempDir, err := os.MkdirTemp(r.config.TempDir, "gram-parquet-*")
		if err != nil {
			return fmt.Errorf("create parquet page directory: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(tempDir); err != nil {
				r.config.Logger.ErrorContext(ctx, "remove parquet page directory", attr.SlogError(err), attr.SlogSubscriptionProtoName(r.def.ProtoName))
			}
		}()

		// Encoded column pages spill to disk while each row group is assembled.
		// Decoding, page construction, compression and output still use memory;
		// completed row groups stream directly to the object store.
		w := parquet.NewWriter(out, r.def.Schema, parquet.Compression(&zstd.Codec{}), parquet.MaxRowsPerRowGroup(parquetRowGroupRows), parquet.PageBufferSize(parquetBufferBytes), parquet.WriteBufferSize(parquetBufferBytes),
			parquet.ColumnPageBuffers(parquet.NewFileBufferPool(tempDir, "column-*")),
			parquet.KeyValueMetadata("gram.mapping_version", declarations.StorageMappingVersion), parquet.KeyValueMetadata("gram.schema_fingerprint", r.def.Fingerprint))
		// Reset releases open page files without flushing or completing a failed
		// object. It runs before directory removal, even on cancellation/panic.
		defer w.Reset(nil)

		for i := start; i < len(messages); i++ {
			if err := ctx.Err(); err != nil {
				return err
			}

			row := first
			if i != start {
				var ok bool
				row, ok = decode(messages[i])
				if !ok {
					continue
				}
			}

			if _, err := w.WriteRows([]parquet.Row{row}); err != nil {
				return fmt.Errorf("write parquet row: %w", err)
			}
			written = append(written, messages[i])
		}

		if err := w.Close(); err != nil {
			return fmt.Errorf("finish parquet footer: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// A successful durable close wins over a racing cancellation. Already
	// committed partitions are never nacked because a sibling partition failed.
	for _, m := range written {
		m.settle(true)
	}
	r.objects.Add(ctx, 1, r.label)
	r.written.Add(ctx, int64(len(written)), r.label)

	return nil
}
