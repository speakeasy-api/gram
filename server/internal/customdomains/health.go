package customdomains

import (
	"fmt"
	"strings"
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

// DNSRemediation carries what a customer needs to fix DNS-shaped health
// issues: the CNAME target for subdomains, the static ingress IPs for apex
// domains (which cannot carry a CNAME), and the domain itself so the record
// type can be suggested. Apex detection is a heuristic, so the copy offers the
// suggested record first without asserting the other is wrong.
type DNSRemediation struct {
	Domain           string
	ExpectedCNAME    string
	ExpectedARecords []string
}

// expectedRecordDescription names the DNS record the customer should create.
// Empty when nothing is configured to point at.
func (r DNSRemediation) expectedRecordDescription() string {
	cname := strings.TrimSuffix(r.ExpectedCNAME, ".")
	aRecords := strings.Join(r.ExpectedARecords, ", ")
	aNoun := "an A record"
	if len(r.ExpectedARecords) > 1 {
		aNoun = "A records"
	}
	apex := len(r.ExpectedARecords) > 0 && IsProbablyApexDomain(r.Domain)
	switch {
	case apex:
		return fmt.Sprintf("%s pointing the domain at %s", aNoun, aRecords)
	case cname != "" && len(r.ExpectedARecords) > 0:
		return fmt.Sprintf("a CNAME record pointing the domain at %s (or, for an apex domain, %s pointing at %s)", cname, aNoun, aRecords)
	case cname != "":
		return fmt.Sprintf("a CNAME record pointing the domain at %s", cname)
	case len(r.ExpectedARecords) > 0:
		return fmt.Sprintf("%s pointing the domain at %s", aNoun, aRecords)
	default:
		return ""
	}
}

// HealthIssueMessage renders the customer-facing description of a health
// issue; product messaging names the exact record instead of the platform.
func HealthIssueMessage(issue HealthIssue, remediation DNSRemediation) string {
	record := remediation.expectedRecordDescription()
	switch issue {
	case HealthIssueDNSNotFound:
		if record != "" {
			return fmt.Sprintf("DNS records for the domain could not be found. Create %s.", record)
		}
		return "DNS records for the domain could not be found."
	case HealthIssueDNSTargetMismatch:
		if record != "" {
			return fmt.Sprintf("The domain's DNS no longer resolves to the expected target. Create %s.", record)
		}
		return "The domain's DNS no longer resolves to the expected target."
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

// Both thresholds must hold: rapid manual rechecks alone cannot reach a week,
// and a week of wall-clock unhealthiness alone is not enough without sustained
// failing checks.
const (
	AutoDisableConsecutiveFailures int32         = 7
	AutoDisableUnhealthyFor        time.Duration = 7 * 24 * time.Hour
)

// ShouldAutoDisable reports whether both auto-disable thresholds are crossed.
// check_failed is excluded: a Gram-side probe failure is not a customer fault.
func ShouldAutoDisable(state HealthState, now time.Time) bool {
	if state.Status != HealthStatusUnhealthy || state.Issue == HealthIssueCheckFailed {
		return false
	}
	if state.ConsecutiveFailures < AutoDisableConsecutiveFailures {
		return false
	}
	return state.UnhealthySince != nil &&
		now.UTC().Sub(state.UnhealthySince.UTC()) >= AutoDisableUnhealthyFor
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
