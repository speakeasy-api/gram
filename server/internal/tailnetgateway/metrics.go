package tailnetgateway

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterScope          = "github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	hostsMetric         = "gram.tailnet_gateway.hosts"
	lifecycleMetric     = "gram.tailnet_gateway.host.lifecycle"
	startDurationMetric = "gram.tailnet_gateway.host.start.duration"
	identityMetric      = "gram.tailnet_gateway.identity"
	pollDurationMetric  = "gram.tailnet_gateway.poll.duration"
)

// Operation names a host lifecycle step recorded in metrics.
type Operation string

const (
	OperationStart     Operation = "start"
	OperationStop      Operation = "stop"
	OperationHeartbeat Operation = "heartbeat"
)

// Result is the outcome of a lifecycle step.
type Result string

const (
	ResultOK    Result = "ok"
	ResultError Result = "error"
)

// Metrics records gateway health per organization. Cardinality is bounded by
// the number of private network customers, so the org slug is an attribute.
type Metrics struct {
	hosts         metric.Int64UpDownCounter
	lifecycle     metric.Int64Counter
	startDuration metric.Float64Histogram
	identity      metric.Int64Counter
	pollDuration  metric.Float64Histogram
}

// NewMetrics registers the gateway instruments. A nil provider disables them.
func NewMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *Metrics {
	if meterProvider == nil {
		return &Metrics{hosts: nil, lifecycle: nil, startDuration: nil, identity: nil, pollDuration: nil}
	}
	meter := meterProvider.Meter(meterScope)
	report := func(name string, err error) {
		if err != nil && logger != nil {
			logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(name), attr.SlogError(err))
		}
	}
	hosts, err := meter.Int64UpDownCounter(hostsMetric,
		metric.WithDescription("Tailnet nodes hosted by this gateway pod by organization, slot and status."),
		metric.WithUnit("{host}"))
	report(hostsMetric, err)
	lifecycle, err := meter.Int64Counter(lifecycleMetric,
		metric.WithDescription("Tailnet gateway host lifecycle operations by organization, operation and result."),
		metric.WithUnit("{operation}"))
	report(lifecycleMetric, err)
	startDuration, err := meter.Float64Histogram(startDurationMetric,
		metric.WithDescription("Seconds from starting a tailnet node until it is advertising its service."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.5, 1, 2.5, 5, 10, 20, 30, 60, 120))
	report(startDurationMetric, err)
	identity, err := meter.Int64Counter(identityMetric,
		metric.WithDescription("Tailnet peers seen by the gateway by organization and identity kind."),
		metric.WithUnit("{request}"))
	report(identityMetric, err)
	pollDuration, err := meter.Float64Histogram(pollDurationMetric,
		metric.WithDescription("Duration of one gateway reconcile pass in seconds."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))
	report(pollDurationMetric, err)
	return &Metrics{hosts: hosts, lifecycle: lifecycle, startDuration: startDuration, identity: identity, pollDuration: pollDuration}
}

// HostStatusChanged moves one host from the previous status bucket to the next.
// An empty previous or next status means the host appeared or disappeared.
func (m *Metrics) HostStatusChanged(ctx context.Context, orgSlug string, slot int32, previous, next NodeStatus) {
	if m == nil || m.hosts == nil || previous == next {
		return
	}
	if previous != "" {
		m.hosts.Add(ctx, -1, metric.WithAttributes(attr.OrganizationSlug(orgSlug), attr.TailnetGatewaySlot(slot), attr.TailnetGatewayNodeStatus(previous)))
	}
	if next != "" {
		m.hosts.Add(ctx, 1, metric.WithAttributes(attr.OrganizationSlug(orgSlug), attr.TailnetGatewaySlot(slot), attr.TailnetGatewayNodeStatus(next)))
	}
}

// Lifecycle counts one host operation.
func (m *Metrics) Lifecycle(ctx context.Context, orgSlug string, operation Operation, result Result) {
	if m == nil || m.lifecycle == nil {
		return
	}
	m.lifecycle.Add(ctx, 1, metric.WithAttributes(attr.OrganizationSlug(orgSlug), attr.TailnetGatewayOperation(operation), attr.TailnetGatewayResult(result)))
}

// StartDuration records how long a node took to come online.
func (m *Metrics) StartDuration(ctx context.Context, orgSlug string, registered bool, duration time.Duration) {
	if m == nil || m.startDuration == nil {
		return
	}
	m.startDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attr.OrganizationSlug(orgSlug), attribute.Bool("gram.tailnet_gateway.registered", registered)))
}

// Identity counts one proxied request by the kind of peer behind it.
func (m *Metrics) Identity(ctx context.Context, orgSlug string, kind IdentityKind) {
	if m == nil || m.identity == nil {
		return
	}
	m.identity.Add(ctx, 1, metric.WithAttributes(attr.OrganizationSlug(orgSlug), attr.TailnetGatewayIdentityKind(kind)))
}

// Poll records one reconcile pass.
func (m *Metrics) Poll(ctx context.Context, duration time.Duration) {
	if m == nil || m.pollDuration == nil {
		return
	}
	m.pollDuration.Record(ctx, duration.Seconds())
}
