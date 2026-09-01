package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/metric"
)

const (
	networkIngressMeterScope        = "github.com/speakeasy-api/gram/server/internal/k8s"
	networkIngressOperationsMetric  = "gram.network_ingress.provisioner.operations"
	networkIngressDurationMetric    = "gram.network_ingress.provisioner.operation.duration"
	networkIngressOperationApply    = "apply"
	networkIngressOperationObserve  = "observe"
	networkIngressOperationDelete   = "delete"
	networkIngressOperationUnknown  = "unknown"
	networkIngressResultSuccess     = "success"
	networkIngressResultError       = "error"
	networkIngressErrorCodeNone     = "none"
	networkIngressErrorCodeInternal = "internal"
)

type NetworkIngressMetrics struct {
	operations metric.Int64Counter
	duration   metric.Float64Histogram
}

func NewNetworkIngressMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *NetworkIngressMetrics {
	if meterProvider == nil {
		return &NetworkIngressMetrics{operations: nil, duration: nil}
	}
	meter := meterProvider.Meter(networkIngressMeterScope)
	operations, err := meter.Int64Counter(
		networkIngressOperationsMetric,
		metric.WithDescription("Network ingress provisioner operations by provider, operation, result, and redacted error code."),
		metric.WithUnit("{operation}"),
	)
	if err != nil && logger != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(networkIngressOperationsMetric), attr.SlogError(err))
	}
	duration, err := meter.Float64Histogram(
		networkIngressDurationMetric,
		metric.WithDescription("Network ingress provisioner operation duration in seconds."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60),
	)
	if err != nil && logger != nil {
		logger.ErrorContext(context.Background(), "failed to create metric", attr.SlogMetricName(networkIngressDurationMetric), attr.SlogError(err))
	}
	return &NetworkIngressMetrics{operations: operations, duration: duration}
}

func (m *NetworkIngressMetrics) Record(ctx context.Context, provider, operation, result, errorCode string, duration time.Duration) {
	if m == nil {
		return
	}
	provider = clampNetworkIngressProvider(provider)
	operation = clampNetworkIngressOperation(operation)
	result = clampNetworkIngressResult(result)
	errorCode = clampNetworkIngressErrorCode(errorCode)
	attributes := metric.WithAttributes(
		attr.Provider(provider),
		attr.NetworkIngressOperation(operation),
		attr.NetworkIngressResult(result),
		attr.NetworkIngressErrorCode(errorCode),
	)
	if m.operations != nil {
		m.operations.Add(ctx, 1, attributes)
	}
	if m.duration != nil {
		m.duration.Record(ctx, duration.Seconds(), attributes)
	}
}

type observedNetworkIngressProvisioner struct {
	provider    string
	provisioner NetworkIngressProvisioner
	logger      *slog.Logger
	metrics     *NetworkIngressMetrics
}

func ObserveNetworkIngressProvisioner(provider string, provisioner NetworkIngressProvisioner, logger *slog.Logger, metrics *NetworkIngressMetrics) NetworkIngressProvisioner {
	if provisioner == nil {
		return nil
	}
	if logger != nil {
		logger = logger.With(attr.SlogComponent("network_ingress_provisioner"))
	}
	return &observedNetworkIngressProvisioner{provider: provider, provisioner: provisioner, logger: logger, metrics: metrics}
}

func (p *observedNetworkIngressProvisioner) Apply(ctx context.Context, desired NetworkIngressDesired) (observation NetworkIngressObservation, err error) {
	started := time.Now()
	defer func() { p.record(ctx, networkIngressOperationApply, observation, err, time.Since(started)) }()
	observation, err = p.provisioner.Apply(ctx, desired)
	if err != nil {
		return observation, fmt.Errorf("apply network ingress resources: %w", err)
	}
	return observation, nil
}

func (p *observedNetworkIngressProvisioner) Observe(ctx context.Context, resources NetworkIngressResourceNames) (observation NetworkIngressObservation, err error) {
	started := time.Now()
	defer func() { p.record(ctx, networkIngressOperationObserve, observation, err, time.Since(started)) }()
	observation, err = p.provisioner.Observe(ctx, resources)
	if err != nil {
		return observation, fmt.Errorf("observe network ingress resources: %w", err)
	}
	return observation, nil
}

func (p *observedNetworkIngressProvisioner) Delete(ctx context.Context, resources NetworkIngressResourceNames) (err error) {
	started := time.Now()
	defer func() {
		p.record(ctx, networkIngressOperationDelete, NetworkIngressObservation{Status: "", DNSName: "", ErrorCode: "", ConnectedAt: nil}, err, time.Since(started))
	}()
	if err = p.provisioner.Delete(ctx, resources); err != nil {
		return fmt.Errorf("delete network ingress resources: %w", err)
	}
	return nil
}

func (p *observedNetworkIngressProvisioner) record(ctx context.Context, operation string, observation NetworkIngressObservation, err error, duration time.Duration) {
	result := networkIngressResultSuccess
	errorCode := networkIngressErrorCodeNone
	if err != nil {
		result = networkIngressResultError
		errorCode = clampNetworkIngressErrorCode(observation.ErrorCode)
	}
	p.metrics.Record(ctx, p.provider, operation, result, errorCode, duration)
	if p.logger == nil {
		return
	}
	attrs := []any{
		attr.SlogProvider(p.provider),
		attr.SlogOutcome(result),
		attr.SlogNetworkIngressOperation(operation),
		attr.SlogNetworkIngressErrorCode(errorCode),
		attr.SlogNetworkIngressDuration(duration),
	}
	if err != nil {
		attrs = append(attrs, attr.SlogError(err))
		p.logger.ErrorContext(ctx, "network ingress provisioner operation failed", attrs...)
		return
	}
	p.logger.InfoContext(ctx, "network ingress provisioner operation completed", attrs...)
}

func clampNetworkIngressProvider(value string) string {
	if value == NetworkIngressProviderTailscale {
		return value
	}
	return "unknown"
}

func clampNetworkIngressOperation(value string) string {
	switch value {
	case networkIngressOperationApply, networkIngressOperationObserve, networkIngressOperationDelete:
		return value
	default:
		return networkIngressOperationUnknown
	}
}

func clampNetworkIngressResult(value string) string {
	if value == networkIngressResultSuccess {
		return value
	}
	return networkIngressResultError
}

func clampNetworkIngressErrorCode(value string) string {
	switch value {
	case networkIngressErrorCodeNone,
		networkIngressErrorCodeInternal,
		NetworkIngressErrorInvalidDesiredState,
		NetworkIngressErrorUnsupportedProvider,
		NetworkIngressErrorInvalidCredentials,
		NetworkIngressErrorKubernetes:
		return value
	default:
		return networkIngressErrorCodeInternal
	}
}
