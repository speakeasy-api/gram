package replicainbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const urnPrefix = "urn:gram:"

// WriterConfig binds a writer to one inbox family and reply type.
type WriterConfig[R any] struct {
	// URNNamespace must match the consuming inbox's namespace.
	URNNamespace string

	// Keyspace must match the consuming inbox's keyspace.
	Keyspace string

	// ReplyTTL is the inbox key expiry refreshed on every write; zero means
	// DefaultReplyTTL.
	ReplyTTL time.Duration

	// Encode serializes a reply.
	Encode func(R) ([]byte, error)

	// CorrelationID returns the request id a reply answers, verified against
	// the return address before the write is honored.
	CorrelationID func(R) string
}

// Writer appends replies to replica-scoped Redis inboxes.
type Writer[R any] struct {
	client *redis.Client
	cfg    WriterConfig[R]
}

// NewWriter builds a reply writer over an existing non-blocking Redis client.
func NewWriter[R any](client *redis.Client, cfg WriterConfig[R]) *Writer[R] {
	if cfg.ReplyTTL <= 0 {
		cfg.ReplyTTL = DefaultReplyTTL
	}
	return &Writer[R]{client: client, cfg: cfg}
}

// Write marshals reply and atomically pipelines RPUSH with the inbox TTL.
func (w *Writer[R]) Write(ctx context.Context, urn string, reply R) error {
	replicaID, id, err := ParseURN(w.cfg.URNNamespace, urn)
	if err != nil {
		return err
	}
	if w.cfg.CorrelationID(reply) != id {
		return fmt.Errorf("reply id %q does not match return address id %q", w.cfg.CorrelationID(reply), id)
	}
	payload, err := w.cfg.Encode(reply)
	if err != nil {
		return fmt.Errorf("marshal reply: %w", err)
	}
	key := Key(w.cfg.Keyspace, replicaID)
	_, err = w.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.RPush(ctx, key, payload)
		pipe.Expire(ctx, key, w.cfg.ReplyTTL)
		return nil
	})
	if err != nil {
		return fmt.Errorf("write reply: %w", err)
	}
	return nil
}

// URN formats a replica-scoped return address for one inbox family.
func URN(namespace, replicaID, id string) string {
	return urnPrefix + namespace + ":" + replicaID + ":" + id
}

// ParseURN extracts the replica and request ids from a return address,
// rejecting addresses from other namespaces or with invalid components.
func ParseURN(namespace, value string) (string, string, error) {
	prefix := urnPrefix + namespace + ":"
	if !strings.HasPrefix(value, prefix) {
		return "", "", fmt.Errorf("invalid reply urn %q", value)
	}
	replicaID, id, ok := strings.Cut(strings.TrimPrefix(value, prefix), ":")
	if !ok || !validReplicaID(replicaID) || id == "" || strings.Contains(id, ":") {
		return "", "", fmt.Errorf("invalid reply urn %q", value)
	}
	return replicaID, id, nil
}

// Key returns the Redis list key for a replica within a keyspace.
func Key(keyspace, replicaID string) string {
	return keyspace + ":" + replicaID
}
