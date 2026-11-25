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

// IsDNSHealthIssue identifies routing findings written by workers that combine
// DNS and infrastructure health. These findings do not establish an infrastructure outage.
func IsDNSHealthIssue(issue HealthIssue) bool {
	return issue == HealthIssueDNSNotFound || issue == HealthIssueDNSTargetMismatch
}

type HealthState struct {
	Status               HealthStatus
	Issue                HealthIssue
	CheckedAt            *time.Time
	UnhealthySince       *time.Time
	CertificateExpiresAt *time.Time
	ConsecutiveFailures  int32

	// DNSConfiguration is the DNS observation recorded with this state. It
	// never changes Status; it only decides whether a certificate issue is
	// held back from alerting while DNS does not point at Speakeasy. Empty
	// when the state was recorded without a DNS observation.
	DNSConfiguration DNSConfigurationStatus
}

type HealthObservation struct {
	Status               HealthStatus
	Issue                HealthIssue
	CertificateExpiresAt *time.Time

	// DNSConfiguration is the DNS observation made alongside this health
	// observation.
	DNSConfiguration DNSConfigurationStatus
}

// isAlertHeldForDNS reports whether an unhealthy issue is expected because the
// domain's DNS does not point at Speakeasy yet. A certificate cannot issue
// until the hostname resolves to Speakeasy, so a domain verified ahead of a
// DNS cutover reads certificate_missing or certificate_not_ready with nothing
// wrong on our side. The alert waits until DNS matches. A failed, missing or
// unconfigured DNS observation cannot tell, so it does not hold the alert. A
// proxy hides the target and always reads unconfirmed, so a proxied domain's
// certificate alerts stay held; the dashboard still shows the issue.
func isAlertHeldForDNS(issue HealthIssue, dns DNSConfigurationStatus) bool {
	if issue != HealthIssueCertificateMissing && issue != HealthIssueCertificateNotReady {
		return false
	}
	switch dns {
	case DNSConfigurationWrongTarget, DNSConfigurationUnconfirmed, DNSConfigurationNotFound:
		return true
	default:
		return false
	}
}

// isAlertable reports whether a state is a customer-actionable problem that
// warrants an unhealthy alert: not a Speakeasy-side probe failure, and not a
// certificate issue held while DNS does not point at Speakeasy.
func isAlertable(state HealthState) bool {
	return state.Status == HealthStatusUnhealthy &&
		state.Issue != HealthIssueCheckFailed &&
		!isAlertHeldForDNS(state.Issue, state.DNSConfiguration)
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

func ShouldNotifyUnhealthyTransition(current, next HealthState) bool {
	// Probe failures are Speakeasy-side and not customer-actionable, and a
	// certificate cannot issue before DNS points at Speakeasy.
	if !isAlertable(next) {
		return false
	}
	if current.Status != HealthStatusUnhealthy {
		return true
	}
	// Already unhealthy: a probe-failure episode or a held certificate issue
	// must not swallow the alert for a customer-actionable issue found by a
	// later check, such as a certificate still not ready once DNS matches.
	return !isAlertable(current)
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
	if !isAlertable(current) {
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
		DNSConfiguration:     observation.DNSConfiguration,
	}
	if observation.Status == HealthStatusHealthy {
		return next
	}

	next.ConsecutiveFailures = 1
	next.UnhealthySince = &checkedAt
	if current.Status == HealthStatusUnhealthy {
		next.ConsecutiveFailures = current.ConsecutiveFailures + 1
		next.UnhealthySince = current.UnhealthySince
		// A probe-failure episode resolving into a real issue, or a held
		// certificate issue becoming alertable once DNS matches, marks the start
		// of the confirmed outage. Re-anchoring UnhealthySince here also lets
		// IsRetryOfUnhealthyTransition recognize a retried commit of this
		// (notifying) transition.
		if (current.Issue == HealthIssueCheckFailed && next.Issue != HealthIssueCheckFailed) || (!isAlertable(current) && isAlertable(next)) {
			next.UnhealthySince = &checkedAt
		}
		if next.UnhealthySince == nil {
			next.UnhealthySince = &checkedAt
		}
	}
	return next
}
