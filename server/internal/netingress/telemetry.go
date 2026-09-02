package netingress

import (
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"go.opentelemetry.io/otel/metric"
)

const (
	NetworkIngressOperationsMetric = networkingress.OperationsMetric
	NetworkIngressDurationMetric   = networkingress.DurationMetric

	OperationAdmission   = networkingress.OperationAdmission
	OperationAttestation = networkingress.OperationAttestation
	OperationProxy       = networkingress.OperationProxy

	ResultAllowed = networkingress.ResultAllowed
	ResultDenied  = networkingress.ResultDenied
	ResultError   = networkingress.ResultError

	ReasonNone                = networkingress.ReasonNone
	ReasonMissingAttestation  = networkingress.ReasonMissingAttestation
	ReasonInvalidSource       = networkingress.ReasonInvalidSource
	ReasonAttestationRejected = networkingress.ReasonAttestationRejected
	ReasonVerifierUnavailable = networkingress.ReasonVerifierUnavailable
	ReasonHostMismatch        = networkingress.ReasonHostMismatch
	ReasonIdentityInvalid     = networkingress.ReasonIdentityInvalid
	ReasonIdentityRequired    = networkingress.ReasonIdentityRequired
	ReasonProviderUnsupported = networkingress.ReasonProviderUnsupported
	ReasonOriginInvalid       = networkingress.ReasonOriginInvalid
	ReasonTokenReadFailed     = networkingress.ReasonTokenReadFailed
	ReasonUpstreamFailed      = networkingress.ReasonUpstreamFailed
	ReasonRateLimited         = networkingress.ReasonRateLimited
	ReasonCacheHit            = networkingress.ReasonCacheHit
	ReasonNegativeCacheHit    = networkingress.ReasonNegativeCacheHit
	ReasonTokenReviewDenied   = networkingress.ReasonTokenReviewDenied
	ReasonAuthorityRejected   = networkingress.ReasonAuthorityRejected
	ReasonDependencyFailed    = networkingress.ReasonDependencyFailed
)

type Telemetry = networkingress.Telemetry

func NewTelemetry(logger *slog.Logger, meterProvider metric.MeterProvider) *Telemetry {
	return networkingress.NewTelemetry(logger, meterProvider)
}
