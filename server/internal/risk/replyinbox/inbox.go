// Package replyinbox binds the generic replica inbox
// (internal/replicainbox) to risk enforcement: EnforcementReply payloads,
// scanner/policy lanes, and the risk URN and metric namespaces.
package replyinbox

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
	"github.com/speakeasy-api/gram/server/internal/replicainbox"
)

const (
	// DefaultPollInterval mirrors the generic inbox default for callers.
	DefaultPollInterval = replicainbox.DefaultPollInterval

	// urnNamespace and keyspace fix the wire-visible names; the pystreams
	// reply writer mirrors both and must stay in lockstep.
	urnNamespace = "risk:enforce"
	keyspace     = "enforce:reply"
	metricPrefix = "risk.enforcement"
)

var ErrDuplicateWaiter = replicainbox.ErrDuplicateWaiter

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
type Outcome = replicainbox.Outcome[Lane, *riskv1.EnforcementReply]

// Stats is a point-in-time snapshot of reply-inbox load and Redis pool state.
type Stats = replicainbox.Stats

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

// Inbox is the risk enforcement instantiation of the generic replica inbox.
type Inbox = replicainbox.Inbox[Lane, *riskv1.EnforcementReply]

// Waiter is a registered enforcement wait, from Inbox.Register.
type Waiter = replicainbox.Waiter[Lane, *riskv1.EnforcementReply]

// Writer appends enforcement replies to replica-scoped Redis inboxes.
type Writer = replicainbox.Writer[*riskv1.EnforcementReply]

func codec() replicainbox.Codec[Lane, *riskv1.EnforcementReply] {
	return replicainbox.Codec[Lane, *riskv1.EnforcementReply]{
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
			return reply.GetScanId()
		},
		Lane: func(reply *riskv1.EnforcementReply) Lane {
			return Lane{Scanner: reply.GetScanner(), PolicyID: reply.GetPolicyId()}
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
	inbox, err := replicainbox.New(ctx, logger, tracerProvider, meterProvider, replicainbox.Config[Lane, *riskv1.EnforcementReply]{
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
	return replicainbox.NewWriter(client, replicainbox.WriterConfig[*riskv1.EnforcementReply]{
		URNNamespace:  urnNamespace,
		Keyspace:      keyspace,
		ReplyTTL:      replicainbox.DefaultReplyTTL,
		Encode:        c.Encode,
		CorrelationID: c.CorrelationID,
	})
}

// ReplyURN formats a replica-scoped enforcement return address.
func ReplyURN(replicaID, scanID string) string {
	return replicainbox.URN(urnNamespace, replicaID, scanID)
}

// ParseReplyURN extracts the replica and scan ids from an enforcement return
// address.
func ParseReplyURN(value string) (string, string, error) {
	replicaID, scanID, err := replicainbox.ParseURN(urnNamespace, value)
	if err != nil {
		return "", "", fmt.Errorf("parse enforcement reply urn: %w", err)
	}
	return replicaID, scanID, nil
}

// InboxKey returns the Redis list key for a replica.
func InboxKey(replicaID string) string {
	return replicainbox.Key(keyspace, replicaID)
}
