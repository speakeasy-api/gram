package redisinbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// toyReply exercises the generic inbox with a payload unrelated to risk
// enforcement, proving the package carries no risk coupling.
type toyReply struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

func toyCodec() Codec[toyReply] {
	return Codec[toyReply]{
		Decode: func(raw []byte) (toyReply, error) {
			var r toyReply
			if err := json.Unmarshal(raw, &r); err != nil {
				return toyReply{}, fmt.Errorf("unmarshal toy reply: %w", err)
			}
			return r, nil
		},
		Encode: func(r toyReply) ([]byte, error) {
			payload, err := json.Marshal(r)
			if err != nil {
				return nil, fmt.Errorf("marshal toy reply: %w", err)
			}
			return payload, nil
		},
		CorrelationID: func(r toyReply) string { return r.ID },
		StatusLabel:   nil,
	}
}

type toyEnv struct {
	redis  *miniredis.Miniredis
	client *redis.Client
	inbox  *Inbox[toyReply]
	writer *Writer[toyReply]
}

func setupToyInbox(t *testing.T, replicaID string, drainFunc func(context.Context)) *toyEnv {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), Protocol: 2})
	t.Cleanup(func() { _ = client.Close() })
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	inbox, err := New(t.Context(), slog.Default(), otel.GetTracerProvider(), meterProvider, Config[toyReply]{ //nolint:forbidigo // toy test logger
		RedisOptions: redis.Options{Addr: mr.Addr(), Protocol: 2},
		ReplicaID:    replicaID,
		PollInterval: DefaultPollInterval,
		URNNamespace: "toy:reply",
		Keyspace:     "toy:inbox",
		MetricPrefix: "toy.requests",
		Component:    "toy-inbox",
		Codec:        toyCodec(),
		DrainGate:    nil,
		drainFunc:    drainFunc,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = inbox.Close() })
	writer := NewWriter(client, WriterConfig[toyReply]{
		URNNamespace:  "toy:reply",
		Keyspace:      "toy:inbox",
		ReplyTTL:      DefaultReplyTTL,
		Encode:        toyCodec().Encode,
		CorrelationID: toyCodec().CorrelationID,
	})
	return &toyEnv{redis: mr, client: client, inbox: inbox, writer: writer}
}

func TestToyInstantiationRoundTrip(t *testing.T) {
	t.Parallel()

	te := setupToyInbox(t, "toy-replica", nil)
	type result struct {
		reply toyReply
		err   error
	}
	done := make(chan result, 1)
	go func() {
		reply, err := te.inbox.Await(t.Context(), "req-1")
		done <- result{reply: reply, err: err}
	}()
	require.Eventually(t, func() bool {
		return te.inbox.Snapshot().Waiters == 1
	}, time.Second, 5*time.Millisecond)
	require.NoError(t, te.writer.Reply(t.Context(), te.inbox.URN("req-1"), toyReply{ID: "req-1", Value: "alpha"}))

	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, "alpha", got.reply.Value)
}

func TestReplyAfterReleaseIsCountedAsOrphan(t *testing.T) {
	t.Parallel()

	te := setupToyInbox(t, "toy-released", nil)
	_, release, err := te.inbox.Register("req-released")
	require.NoError(t, err)
	release()

	raw, err := json.Marshal(toyReply{ID: "req-released", Value: "alpha"})
	require.NoError(t, err)
	te.inbox.route(t.Context(), string(raw))

	require.Equal(t, uint64(1), te.inbox.Snapshot().OrphanedReplies)
}

func TestDuplicateReplyIsCountedAsOrphan(t *testing.T) {
	t.Parallel()

	te := setupToyInbox(t, "toy-overflow", nil)
	w, release, err := te.inbox.Register("req-overflow")
	require.NoError(t, err)
	defer release()

	raw, err := json.Marshal(toyReply{ID: "req-overflow", Value: "alpha"})
	require.NoError(t, err)
	te.inbox.route(t.Context(), string(raw))
	te.inbox.route(t.Context(), string(raw))

	require.Len(t, w.reply, 1)
	require.Equal(t, uint64(1), te.inbox.Snapshot().OrphanedReplies)
}

func TestDedicatedClientUsesBoundedPool(t *testing.T) {
	t.Parallel()

	te := setupToyInbox(t, "toy-pool", nil)
	require.Equal(t, defaultPoolSize, te.inbox.client.Options().PoolSize)
}

func TestDrainerSupervisorRestartsAfterPanic(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	run := func(ctx context.Context) {
		if attempts.Add(1) == 1 {
			panic("synthetic drainer panic")
		}
		<-ctx.Done()
	}
	te := setupToyInbox(t, "toy-supervised", run)

	require.Eventually(t, func() bool {
		stats := te.inbox.Snapshot()
		return attempts.Load() >= 2 && stats.DrainerAlive && stats.DrainerErrors == 1
	}, 2*time.Second, 5*time.Millisecond)
}

func TestMissingCodecOrNamespaceIsRejected(t *testing.T) {
	t.Parallel()

	_, err := New(t.Context(), slog.Default(), otel.GetTracerProvider(), otel.GetMeterProvider(), Config[toyReply]{ //nolint:forbidigo // toy test logger
		RedisOptions: redis.Options{Addr: "127.0.0.1:1"},
		ReplicaID:    "toy",
		PollInterval: DefaultPollInterval,
		URNNamespace: "toy:reply",
		Keyspace:     "toy:inbox",
		MetricPrefix: "toy.requests",
		Component:    "",
		Codec:        Codec[toyReply]{Decode: nil, Encode: nil, CorrelationID: nil, StatusLabel: nil},
		DrainGate:    nil,
		drainFunc:    nil,
	})
	require.ErrorContains(t, err, "codec")

	cfg := Config[toyReply]{
		RedisOptions: redis.Options{Addr: "127.0.0.1:1"},
		ReplicaID:    "toy",
		PollInterval: DefaultPollInterval,
		URNNamespace: "",
		Keyspace:     "toy:inbox",
		MetricPrefix: "toy.requests",
		Component:    "",
		Codec:        toyCodec(),
		DrainGate:    nil,
		drainFunc:    nil,
	}
	_, err = New(t.Context(), slog.Default(), otel.GetTracerProvider(), otel.GetMeterProvider(), cfg) //nolint:forbidigo // toy test logger
	require.ErrorContains(t, err, "namespace")
}
