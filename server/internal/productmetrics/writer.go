package productmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	pmv1 "github.com/speakeasy-api/gram/infra/gen/gram/productmetrics/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/metric"
)

const (
	// RollupRetention is the event-time horizon supported by serving queries.
	RollupRetention = 90 * 24 * time.Hour
	// MaxFutureSkew permits clock skew without creating arbitrarily future partitions.
	MaxFutureSkew = 5 * time.Minute
	// BatchMaxMessages bounds pending observations per insert.
	BatchMaxMessages = 1000
	// BatchMaxBytes triggers flush at 10 MiB of wire payloads.
	BatchMaxBytes = 10 * 1024 * 1024
	// MaxOutstandingBytes bounds the subscriber's wire buffers at 20 MiB.
	MaxOutstandingBytes = 20 * 1024 * 1024
)

// Inserter durably inserts validated observations, awaiting materialized views.
type Inserter interface {
	// Insert writes all supplied contributions or returns a retryable error.
	Insert(context.Context, []Contribution) error
}

// Writer validates individual messages and suppresses identical identities within
// a batch. It holds no cross-process or cross-batch deduplication cache.
type Writer struct {
	logger   *slog.Logger
	registry *Registry
	inserter Inserter
	now      func() time.Time
	outcomes metric.Int64Counter
}

// NewWriter creates a batch subscriber with bounded outcome labels. The registry
// contains only code-owned definitions, never definitions learned from messages.
func NewWriter(logger *slog.Logger, provider metric.MeterProvider, registry *Registry, inserter Inserter, now func() time.Time) (*Writer, error) {
	outcomes, err := provider.Meter("github.com/speakeasy-api/gram/server/internal/productmetrics").Int64Counter("gram.product_metrics.contributions", metric.WithUnit("{contribution}"), metric.WithDescription("Contribution outcomes: inserted, invalid, unregistered, expired, future, duplicate, identity_conflict, retry"))
	if err != nil {
		return nil, fmt.Errorf("create product metrics counter: %w", err)
	}
	return &Writer{logger: logger.With(attr.SlogComponent("product-metrics-writer")), registry: registry, inserter: inserter, now: now, outcomes: outcomes}, nil
}

// EarliestBucket rounds the retention cutoff up, excluding partially expired
// minutes. TTL deletion is asynchronous, so queries enforce the same boundary.
func EarliestBucket(now time.Time) time.Time {
	cutoff := now.UTC().Add(-RollupRetention)
	minute := cutoff.Truncate(time.Minute)
	if minute.Before(cutoff) {
		return minute.Add(time.Minute)
	}
	return minute
}

type deliveryIdentity struct {
	tenant  Tenant
	scope   string
	version string
	id      string
}

type observationIdentity struct {
	series     Series
	value      Number
	eventTime  int64
	observedAt int64
}

// HandleBatchWithResult acknowledges invalid messages individually even when
// insertion fails. Valid deliveries are settled only after synchronous insertion.
func (w *Writer) HandleBatchWithResult(ctx context.Context, batch []gcp.BatchMessage[*pmv1.Contribution]) error {
	messages := make([]*pmv1.Contribution, len(batch))
	for i, m := range batch {
		messages[i] = m.Message
	}
	indexes, err := w.process(ctx, messages)
	if err != nil {
		for _, i := range indexes {
			batch[i].Fail(err)
		}
	}
	return nil
}

func (w *Writer) process(ctx context.Context, messages []*pmv1.Contribution) ([]int, error) {
	now := w.now().UTC()
	rows := make([]Contribution, 0, len(messages))
	valid := make([]int, 0, len(messages))
	seen := make(map[deliveryIdentity]observationIdentity, len(messages))
	counts := make(map[string]int64)
	for i, m := range messages {
		c, s, err := Decode(m)
		reason := ""
		switch {
		case err != nil:
			reason = "invalid"
		case w.registry == nil || !w.registry.contains(c.Definition):
			reason = "unregistered"
		case w.registry.ValidateDimensions(c) != nil:
			reason = "invalid"
		case c.EventTime.Before(EarliestBucket(now)):
			reason = "expired"
		case c.EventTime.After(now.Add(MaxFutureSkew)):
			reason = "future"
		}
		if reason != "" {
			counts[reason]++
			continue
		}
		id := deliveryIdentity{tenant: c.Tenant, scope: c.Definition.ScopeName, version: c.Definition.ScopeVersion, id: c.ID}
		observation := observationIdentity{series: s, value: c.Value, eventTime: c.EventTime.UnixNano(), observedAt: c.ObservedAt.UnixNano()}
		if previous, ok := seen[id]; ok {
			if previous != observation {
				counts["identity_conflict"]++
				continue
			}
			counts["duplicate"]++
			valid = append(valid, i)
			continue
		}
		seen[id] = observation
		valid = append(valid, i)
		rows = append(rows, c)
	}
	var insertErr error
	if len(rows) > 0 {
		insertErr = w.inserter.Insert(ctx, rows)
		if insertErr != nil {
			counts["retry"] = int64(len(valid))
		} else {
			counts["inserted"] = int64(len(rows))
		}
	}
	for reason, count := range counts {
		w.outcomes.Add(ctx, count, metric.WithAttributes(attr.Outcome(reason)))
		if reason != "inserted" && reason != "duplicate" && reason != "retry" {
			w.logger.WarnContext(ctx, "discarded metric contributions", attr.SlogReason(reason))
		}
	}
	if insertErr != nil {
		return valid, fmt.Errorf("insert product metric batch: %w", insertErr)
	}
	return nil, nil
}
