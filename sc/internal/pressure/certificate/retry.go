package certificate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// certificateStartWait is how long Start waits for the certificate after
	// approval. First issuance usually takes a few seconds, so a normal start
	// reports online at its first heartbeat instead of briefly reporting a
	// certificate wait.
	certificateStartWait = 30 * time.Second

	// certificateFetchTimeout bounds one request, which runs the full ACME
	// exchange when no certificate is stored yet. It is the Within budget of
	// the Requesting state, which its steps share.
	certificateFetchTimeout = 2 * time.Minute

	// certificateAwaitPollInterval is how often Await checks for the
	// certificate while it waits.
	certificateAwaitPollInterval = 250 * time.Millisecond

	// certificateDomainRetry is the retry after control has not yet listed the
	// service name among the node's certificate domains, which it does shortly
	// after approval.
	certificateDomainRetry = 15 * time.Second

	// certificateIssuingRetry is the retry while another slot holds the
	// issuance lease; first issuance usually takes a few seconds.
	certificateIssuingRetry = 5 * time.Second

	// certificateRefreshInterval is how often, give or take a tenth, a node
	// holding the certificate re-reads the store, which picks up another
	// slot's renewal, and checks whether its own is due.
	certificateRefreshInterval = time.Hour

	// certificateRetryMin and certificateRetryMax bound the backoff after any
	// other failure. The maximum stays under an hour, so a slot notices a
	// recovered issuer within the hour.
	certificateRetryMin = 30 * time.Second
	certificateRetryMax = 30 * time.Minute
)

// acmeRetryAfterPattern finds the time Let's Encrypt names in a rate-limit
// refusal, such as "retry after 2026-11-12 22:53:32 UTC".
var acmeRetryAfterPattern = regexp.MustCompile(`retry after (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)

// isCertificateRateLimited reports whether Let's Encrypt refused the request
// under one of its rate limits.
func isCertificateRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "rateLimited")
}

// isCertificateDomainPending reports whether control has not yet listed the
// service name among the node's certificate domains, as just after approval.
func isCertificateDomainPending(err error) bool {
	return err != nil && strings.Contains(err.Error(), "invalid domain")
}

// nextCertificateRetry is when to request again after the failures-th failure
// in a row. A rate-limit refusal names its own retry time; control not yet
// listing the service name clears within seconds; anything else backs off.
func nextCertificateRetry(now time.Time, err error, failures int) time.Time {
	if isCertificateRateLimited(err) {
		if match := acmeRetryAfterPattern.FindStringSubmatch(err.Error()); match != nil {
			if at, parseErr := time.ParseInLocation(time.DateTime, match[1], time.UTC); parseErr == nil && at.After(now) {
				return at
			}
		}
		return now.Add(certificateRetryMax)
	}
	if isCertificateDomainPending(err) {
		return now.Add(certificateDomainRetry)
	}
	if errors.Is(err, errCertificateIssuing) {
		return now.Add(certificateIssuingRetry)
	}

	backoff := certificateRetryMin
	for i := 1; i < max(failures, 1) && backoff < certificateRetryMax; i++ {
		backoff *= 2
	}
	return now.Add(min(backoff, certificateRetryMax))
}

// certificateFailureMessage is the slot's last_error while it waits for a
// certificate.
func certificateFailureMessage(domain string, err error, retryAt time.Time) string {
	if errors.Is(err, errCertificateIssuing) {
		return "another gateway slot is obtaining the TLS certificate for " + domain
	}
	retry := retryAt.UTC().Format("2006-01-02 15:04 UTC")
	if isCertificateRateLimited(err) {
		return fmt.Sprintf("Let's Encrypt is rate limiting new certificates for this tailnet; retrying after %s", retry)
	}
	return fmt.Sprintf("could not obtain a TLS certificate for %s: %v; retrying after %s", domain, err, retry)
}

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

// certificateOperation names a service counted in metrics; re-reading the
// store is not a certificate request.
func certificateOperation(service string) (CertificateOperation, bool) {
	switch service {
	case serviceObtain:
		return CertificateOperationObtain, true
	case serviceRenew:
		return CertificateOperationRenew, true
	case serviceWindow:
		return CertificateOperationWindow, true
	}
	return "", false
}
