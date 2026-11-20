package tailnetgateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
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

	// certificateRemainingMetric is the alerting signal for certificates:
	// seconds until each running node's certificate expires, negative once
	// it has. Alert on its minimum per ingress well before zero, and on no
	// data, since a connection nobody hosts reports nothing.
	certificateRemainingMetric = "gram.tailnet_gateway.certificate.remaining"

	// certificateRequestsMetric counts certificate requests by outcome, so a
	// renewal that keeps failing shows long before the certificate expires.
	certificateRequestsMetric = "gram.tailnet_gateway.certificate.requests"
)

// CertificateOperation is a certificate request a node makes.
type CertificateOperation string

const (
	CertificateOperationObtain CertificateOperation = "obtain"
	CertificateOperationRenew  CertificateOperation = "renew"
	CertificateOperationWindow CertificateOperation = "window"
)

// CertificateResult is how a certificate request ended.
type CertificateResult string

const (
	CertificateResultOK             CertificateResult = "ok"
	CertificateResultRateLimited    CertificateResult = "rate_limited"
	CertificateResultWaitingForSlot CertificateResult = "waiting_for_slot"
	CertificateResultError          CertificateResult = "error"
)

// certificateResult classifies a certificate request's error.
func certificateResult(err error) CertificateResult {
	switch {
	case err == nil:
		return CertificateResultOK
	case isCertificateRateLimited(err):
		return CertificateResultRateLimited
	case errors.Is(err, errCertificateIssuing):
		return CertificateResultWaitingForSlot
	default:
		return CertificateResultError
	}
}

// CertificateObservation is one running node's certificate, for the expiry
// gauge.
type CertificateObservation struct {
	OrganizationSlug string
	IngressID        uuid.UUID
	Slot             int32
	ExpiresAt        time.Time
}

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

	meter                metric.Meter
	certificateRemaining metric.Float64ObservableGauge
	certificateRequests  metric.Int64Counter
}

// NewMetrics registers the gateway instruments. A nil provider disables them.
func NewMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *Metrics {
	if meterProvider == nil {
		return &Metrics{hosts: nil, lifecycle: nil, startDuration: nil, identity: nil, pollDuration: nil, meter: nil, certificateRemaining: nil, certificateRequests: nil}
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
	certificateRemaining, err := meter.Float64ObservableGauge(certificateRemainingMetric,
		metric.WithDescription("Seconds until each running tailnet node's TLS certificate expires, by organization, ingress and slot; negative once expired."),
		metric.WithUnit("s"))
	report(certificateRemainingMetric, err)
	certificateRequests, err := meter.Int64Counter(certificateRequestsMetric,
		metric.WithDescription("Tailnet node certificate requests by organization, operation (obtain, renew, window) and result."),
		metric.WithUnit("{request}"))
	report(certificateRequestsMetric, err)
	return &Metrics{
		hosts: hosts, lifecycle: lifecycle, startDuration: startDuration, identity: identity, pollDuration: pollDuration,
		meter: meter, certificateRemaining: certificateRemaining, certificateRequests: certificateRequests,
	}
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

// ObserveCertificates reports the expiry gauge from list at every collection.
// Unregister the returned registration when the gateway stops.
func (m *Metrics) ObserveCertificates(now func() time.Time, list func() []CertificateObservation) (metric.Registration, error) {
	if m == nil || m.meter == nil || m.certificateRemaining == nil {
		return nil, nil
	}
	registration, err := m.meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		at := now()
		for _, c := range list() {
			observer.ObserveFloat64(m.certificateRemaining, c.ExpiresAt.Sub(at).Seconds(), metric.WithAttributes(
				attr.OrganizationSlug(c.OrganizationSlug), attr.NetworkIngressID(c.IngressID.String()), attr.TailnetGatewaySlot(c.Slot)))
		}
		return nil
	}, m.certificateRemaining)
	if err != nil {
		return nil, fmt.Errorf("register certificate expiry callback: %w", err)
	}
	return registration, nil
}

// CertificateRequest counts one certificate request.
func (m *Metrics) CertificateRequest(ctx context.Context, orgSlug string, operation CertificateOperation, result CertificateResult) {
	if m == nil || m.certificateRequests == nil {
		return
	}
	m.certificateRequests.Add(ctx, 1, metric.WithAttributes(attr.OrganizationSlug(orgSlug), attr.TailnetGatewayOperation(operation), attr.TailnetGatewayResult(result)))
}
