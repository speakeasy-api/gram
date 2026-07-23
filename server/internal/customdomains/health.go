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

// Reuse Kubernetes issue values so persisted health issues cannot drift.
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

func HealthIssueMessage(issue HealthIssue) string {
	switch issue {
	case HealthIssueDNSNotFound:
		return "DNS records for the domain could not be found."
	case HealthIssueDNSTargetMismatch:
		return "The domain's DNS no longer resolves to Gram's endpoint."
	case HealthIssueResourceMissing:
		return "The routing configuration for the domain is missing."
	case HealthIssueCertificateMissing,
		HealthIssueCertificateNotReady,
		HealthIssueCertificateExpired,
		HealthIssueCertificateInvalid:
		return "There is a problem with the domain's TLS certificate. We're working to resolve it."
	default:
		return "The latest health check found a problem with the domain."
	}
}

func ShouldNotifyUnhealthyTransition(current, next HealthState) bool {
	// Probe failures are Gram-side and not customer-actionable.
	if next.Status != HealthStatusUnhealthy || next.Issue == HealthIssueCheckFailed {
		return false
	}
	if current.Status != HealthStatusUnhealthy {
		return true
	}
	// Already unhealthy: a probe-failure episode must not swallow the alert for
	// a real, customer-actionable issue discovered by a later successful probe.
	return current.Issue == HealthIssueCheckFailed
}

// IsRetryOfUnhealthyTransition reports whether the persisted state shows that
// an unhealthy transition was already committed by a check at exactly
// checkedAt. The check activity can commit its transition and then die before
// Temporal records completion; the retry re-runs with the same pinned
// checkedAt, sees no state change, and would otherwise drop the one-shot
// notification. UnhealthySince == CheckedAt == checkedAt uniquely identifies
// "this very check committed a notifying transition" — ReconcileHealthState
// anchors UnhealthySince at every such transition, including a check_failed
// episode resolving into a real issue — so the retry re-emits the same
// notification args and the activity stays idempotent.
func IsRetryOfUnhealthyTransition(current HealthState, checkedAt time.Time) bool {
	if current.Status != HealthStatusUnhealthy || current.Issue == HealthIssueCheckFailed {
		return false
	}
	checkedAt = checkedAt.UTC()
	return current.CheckedAt != nil && checkedAt.Equal(*current.CheckedAt) &&
		current.UnhealthySince != nil && checkedAt.Equal(*current.UnhealthySince)
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
		// A probe-failure episode resolving into a real issue marks the start of
		// the confirmed outage. Re-anchoring UnhealthySince here also lets
		// IsRetryOfUnhealthyTransition recognize a retried commit of this
		// (notifying) transition.
		if current.Issue == HealthIssueCheckFailed && next.Issue != HealthIssueCheckFailed {
			next.UnhealthySince = &checkedAt
		}
		if next.UnhealthySince == nil {
			next.UnhealthySince = &checkedAt
		}
	}
	return next
}
