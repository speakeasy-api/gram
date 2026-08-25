package replyinbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
)

const replyURNPrefix = "urn:gram:risk:enforce:"

// Writer appends enforcement replies to replica-scoped Redis inboxes.
type Writer struct {
	client *redis.Client
}

// NewWriter builds a reply writer over an existing non-blocking Redis client.
func NewWriter(client *redis.Client) *Writer {
	return &Writer{client: client}
}

// Write marshals reply and atomically pipelines RPUSH with the inbox TTL.
func (w *Writer) Write(ctx context.Context, replyURN string, reply *riskv1.EnforcementReply) error {
	replicaID, scanID, err := ParseReplyURN(replyURN)
	if err != nil {
		return err
	}
	if reply.GetScanId() != scanID {
		return fmt.Errorf("reply scan id %q does not match return address scan id %q", reply.GetScanId(), scanID)
	}
	payload, err := proto.Marshal(reply)
	if err != nil {
		return fmt.Errorf("marshal enforcement reply: %w", err)
	}
	_, err = w.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.RPush(ctx, InboxKey(replicaID), payload)
		pipe.Expire(ctx, InboxKey(replicaID), defaultReplyTTL)
		return nil
	})
	if err != nil {
		return fmt.Errorf("write enforcement reply: %w", err)
	}
	return nil
}

// ReplyURN formats a replica-scoped enforcement return address.
func ReplyURN(replicaID, scanID string) string {
	return replyURNPrefix + replicaID + ":" + scanID
}

// ParseReplyURN extracts the replica and scan ids from an enforcement return address.
func ParseReplyURN(value string) (string, string, error) {
	if !strings.HasPrefix(value, replyURNPrefix) {
		return "", "", fmt.Errorf("invalid enforcement reply urn %q", value)
	}
	replicaID, scanID, ok := strings.Cut(strings.TrimPrefix(value, replyURNPrefix), ":")
	if !ok || !validReplicaID(replicaID) || scanID == "" || strings.Contains(scanID, ":") {
		return "", "", fmt.Errorf("invalid enforcement reply urn %q", value)
	}
	return replicaID, scanID, nil
}

// InboxKey returns the Redis list key for a replica.
func InboxKey(replicaID string) string {
	return "enforce:reply:" + replicaID
}
