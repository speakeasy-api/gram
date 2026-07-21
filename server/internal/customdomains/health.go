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
