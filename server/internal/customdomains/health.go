package customdomains

import (
	"time"

	"github.com/speakeasy-api/gram/server/internal/k8s"
)

type HealthStatus string

const (
	HealthStatusUnknown   HealthStatus = "unknown"
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

type HealthIssue string

// Infrastructure issues alias the k8s constants so the two packages cannot
// drift apart; the activity casts k8s issues straight into HealthIssue.
const (
	HealthIssueDNSNotFound         HealthIssue = "dns_not_found"
	HealthIssueDNSTargetMismatch   HealthIssue = "dns_target_mismatch"
	HealthIssueResourceMissing     HealthIssue = HealthIssue(k8s.CustomDomainInfrastructureIssueResourceMissing)
	HealthIssueCertificateMissing  HealthIssue = HealthIssue(k8s.CustomDomainInfrastructureIssueCertificateMissing)
	HealthIssueCertificateNotReady HealthIssue = HealthIssue(k8s.CustomDomainInfrastructureIssueCertificateNotReady)
	HealthIssueCertificateExpired  HealthIssue = HealthIssue(k8s.CustomDomainInfrastructureIssueCertificateExpired)
	HealthIssueCertificateInvalid  HealthIssue = HealthIssue(k8s.CustomDomainInfrastructureIssueCertificateInvalid)
	HealthIssueCheckFailed         HealthIssue = "check_failed"
)

type HealthState struct {
	Status               HealthStatus
	Issue                HealthIssue
	CheckedAt            *time.Time
	UnhealthySince       *time.Time
	CertificateExpiresAt *time.Time
	ConsecutiveFailures  int32
}

type HealthObservation struct {
	Status               HealthStatus
	Issue                HealthIssue
	CertificateExpiresAt *time.Time
}

// HealthIssueMessage renders a health issue as human-readable copy for
// notifications. check_failed intentionally maps to the generic fallback:
// callers should not notify customers about Gram-side check failures.
func HealthIssueMessage(issue HealthIssue) string {
	switch issue {
	case HealthIssueDNSNotFound:
		return "DNS records for the domain could not be found."
	case HealthIssueDNSTargetMismatch:
		return "The domain's DNS no longer resolves to Gram's endpoint."
	case HealthIssueResourceMissing:
		return "The routing configuration for the domain is missing."
	case HealthIssueCertificateMissing:
		return "The TLS certificate for the domain is missing."
	case HealthIssueCertificateNotReady:
		return "The TLS certificate for the domain is not ready."
	case HealthIssueCertificateExpired:
		return "The TLS certificate for the domain has expired."
	case HealthIssueCertificateInvalid:
		return "The TLS certificate does not match the domain or could not be read."
	default:
		return "The latest health check found a problem with the domain."
	}
}

// ShouldNotifyUnhealthyTransition reports whether reconciling produced a
// fresh healthy-to-unhealthy transition worth alerting the organization
// about. check_failed transitions are excluded: they represent Gram-side
// probe failures, not customer misconfiguration.
func ShouldNotifyUnhealthyTransition(current, next HealthState) bool {
	if next.Status != HealthStatusUnhealthy || current.Status == HealthStatusUnhealthy {
		return false
	}
	return next.Issue != HealthIssueCheckFailed
}

func ReconcileHealthState(current HealthState, observation HealthObservation, checkedAt time.Time) HealthState {
	checkedAt = checkedAt.UTC()
	if current.CheckedAt != nil && !checkedAt.After(*current.CheckedAt) {
		return current
	}

	next := HealthState{
		Status:               observation.Status,
		Issue:                observation.Issue,
		CheckedAt:            &checkedAt,
		UnhealthySince:       nil,
		CertificateExpiresAt: observation.CertificateExpiresAt,
		ConsecutiveFailures:  0,
	}
	if observation.Status == HealthStatusHealthy {
		return next
	}

	next.ConsecutiveFailures = 1
	next.UnhealthySince = &checkedAt
	if current.Status == HealthStatusUnhealthy {
		next.ConsecutiveFailures = current.ConsecutiveFailures + 1
		next.UnhealthySince = current.UnhealthySince
		if next.UnhealthySince == nil {
			next.UnhealthySince = &checkedAt
		}
	}
	return next
}
