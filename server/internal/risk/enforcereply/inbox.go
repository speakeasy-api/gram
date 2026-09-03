// Package enforcereply binds the generic Redis inbox
// (internal/redisinbox) to risk enforcement replies and return addresses.
package enforcereply

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/redisinbox"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
)

const (
	// DefaultPollInterval mirrors the generic inbox default for callers.
	DefaultPollInterval = redisinbox.DefaultPollInterval

	// urnNamespace and keyspace fix the wire-visible names; the pystreams
	// reply writer mirrors both and must stay in lockstep.
	urnNamespace = "risk:enforce"
	keyspace     = "enforce:reply"
	metricPrefix = "risk.enforcement"
)

var ErrDuplicateWaiter = redisinbox.ErrDuplicateWaiter

// Lane identifies one distinct enforcement result. PolicyID is empty except
// for policy-specific lanes such as judge.
type Lane struct {
	// Scanner identifies the enforcement engine.
	Scanner riskv1.EnforcementScanner

	// PolicyID distinguishes multiple policy-specific results from one scanner.
	PolicyID string
}

// String returns a stable diagnostic representation of a lane.
func (l Lane) String() string {
	if l.PolicyID == "" {
		return l.Scanner.String()
	}
	return l.Scanner.String() + "/" + l.PolicyID
}

// Outcome folds complete and deadline-limited enforcement replies by lane.
type Outcome struct {
	// ByLane contains the first reply received for each requested lane.
	ByLane map[Lane]*riskv1.EnforcementReply

	// Complete reports whether every requested lane replied.
	Complete bool

	// Deadline reports that at least one lane reached its wait deadline.
	Deadline bool
}

// Stats is a point-in-time snapshot of reply-inbox load and Redis pool state.
type Stats = redisinbox.Stats

// Config controls a replica's dedicated reply-drain Redis client.
type Config struct {
	// RedisOptions are copied before reply-inbox settings are applied.
	RedisOptions redis.Options

	// ReplicaID identifies the process in reply URNs and Redis inbox keys.
	ReplicaID string

	// PollInterval paces the generic inbox's waiter-gated LPOP polling.
	PollInterval time.Duration

	// DrainGate supports controlled backlog tests; nil in production.
	DrainGate <-chan struct{}
}

// Inbox is the risk enforcement instantiation of the generic Redis inbox.
type Inbox = redisinbox.Inbox[*riskv1.EnforcementReply]

// Waiter is a registered enforcement wait, from Inbox.Register.
type Waiter = redisinbox.Waiter[*riskv1.EnforcementReply]

// Writer appends enforcement replies to replica-scoped Redis inboxes.
type Writer = redisinbox.Writer[*riskv1.EnforcementReply]

func codec() redisinbox.Codec[*riskv1.EnforcementReply] {
	return redisinbox.Codec[*riskv1.EnforcementReply]{
		Decode: func(raw []byte) (*riskv1.EnforcementReply, error) {
			reply := new(riskv1.EnforcementReply)
			if err := proto.Unmarshal(raw, reply); err != nil {
				return nil, fmt.Errorf("unmarshal enforcement reply: %w", err)
			}
			return reply, nil
		},
		Encode: func(reply *riskv1.EnforcementReply) ([]byte, error) {
			payload, err := proto.Marshal(reply)
			if err != nil {
				return nil, fmt.Errorf("marshal enforcement reply: %w", err)
			}
			return payload, nil
		},
		CorrelationID: func(reply *riskv1.EnforcementReply) string {
			return reply.GetCorrelationId()
		},
		StatusLabel: func(reply *riskv1.EnforcementReply) string {
			return strings.ToLower(strings.TrimPrefix(reply.GetStatus().String(), "ENFORCEMENT_STATUS_"))
		},
	}
}

// New starts one enforcement reply drainer for a stable process replica id.
func New(
	ctx context.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	cfg Config,
) (*Inbox, error) {
	inbox, err := redisinbox.New(ctx, logger, tracerProvider, meterProvider, redisinbox.Config[*riskv1.EnforcementReply]{
		RedisOptions: cfg.RedisOptions,
		ReplicaID:    cfg.ReplicaID,
		PollInterval: cfg.PollInterval,
		URNNamespace: urnNamespace,
		Keyspace:     keyspace,
		MetricPrefix: metricPrefix,
		Component:    "enforcement-reply-inbox",
		Codec:        codec(),
		DrainGate:    cfg.DrainGate,
	})
	if err != nil {
		return nil, fmt.Errorf("create enforcement reply inbox: %w", err)
	}
	return inbox, nil
}

// NewWriter builds an enforcement reply writer over an existing non-blocking
// Redis client.
func NewWriter(client *redis.Client) *Writer {
	c := codec()
	return redisinbox.NewWriter(client, redisinbox.WriterConfig[*riskv1.EnforcementReply]{
		URNNamespace:  urnNamespace,
		Keyspace:      keyspace,
		ReplyTTL:      redisinbox.DefaultReplyTTL,
		Encode:        c.Encode,
		CorrelationID: c.CorrelationID,
	})
}

// ReplyURN formats a replica-scoped enforcement return address.
func ReplyURN(replicaID, correlationID string) string {
	return redisinbox.URN(urnNamespace, replicaID, correlationID)
}

// ParseReplyURN extracts the replica and correlation ids from an enforcement
// return address.
func ParseReplyURN(value string) (string, string, error) {
	replicaID, correlationID, err := redisinbox.ParseURN(urnNamespace, value)
	if err != nil {
		return "", "", fmt.Errorf("parse enforcement reply urn: %w", err)
	}
	return replicaID, correlationID, nil
}

// InboxKey returns the Redis list key for a replica.
func InboxKey(replicaID string) string {
	return redisinbox.Key(keyspace, replicaID)
}

var _ requestreply.ReplyBroker[*riskv1.EnforcementReply] = (*Writer)(nil)
