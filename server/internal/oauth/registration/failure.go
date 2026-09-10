// Package registration defines the failure contract for automatic OAuth client
// registration mechanisms.
package registration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// ProviderMessageMaxRunes bounds provider-controlled text returned to an
// administrator. The message is never suitable for telemetry dimensions.
const ProviderMessageMaxRunes = 256

// Method identifies an automatic OAuth client registration mechanism.
type Method string

const (
	// MethodDCR is RFC 7591 Dynamic Client Registration.
	MethodDCR Method = "dcr"

	// MethodCIMD is OAuth Client ID Metadata Document registration.
	MethodCIMD Method = "cimd"
)

// Outcome is the completed, administrator-facing failure outcome.
type Outcome string

const (
	// OutcomeUnreachable means the provider could not complete registration due
	// to transport, throttling, timeout, or availability conditions.
	OutcomeUnreachable Outcome = "unreachable"

	// OutcomeRefused means the provider rejected registration or returned a
	// successful response that Gram cannot use.
	OutcomeRefused Outcome = "refused"
)

// Reason is a bounded machine-readable explanation for an Outcome.
type Reason string

const (
	ReasonDNSError               Reason = "dns_error"
	ReasonTLSError               Reason = "tls_error"
	ReasonTimeout                Reason = "timeout"
	ReasonNetworkError           Reason = "network_error"
	ReasonRateLimited            Reason = "rate_limited"
	ReasonUpstreamUnavailable    Reason = "upstream_unavailable"
	ReasonAuthorizationRejected  Reason = "authorization_rejected"
	ReasonInvalidSuccessResponse Reason = "invalid_success_response"
)

// Failure is the reusable result returned for a completed automatic
// registration failure. HTTPStatus and ProviderMessage are absent when the
// provider did not supply them.
type Failure struct {
	// Outcome is either unreachable or refused.
	Outcome Outcome

	// Reason is a bounded machine-readable explanation.
	Reason Reason

	// Retryable reports whether repeating the same operation may succeed.
	Retryable bool

	// HTTPStatus is the provider response status, when a response was received.
	HTTPStatus *int

	// ProviderMessage is sanitized, single-line provider-controlled text.
	ProviderMessage *string
}

// HTTPError represents a non-success response from a registration endpoint.
type HTTPError struct {
	// StatusCode is the non-success response status.
	StatusCode int

	// ProviderMessage is sanitized provider-controlled error text.
	ProviderMessage string
}

func (e *HTTPError) Error() string {
	if e.ProviderMessage == "" {
		return fmt.Sprintf("registration endpoint returned %d", e.StatusCode)
	}
	return fmt.Sprintf("registration endpoint returned %d: %s", e.StatusCode, e.ProviderMessage)
}

// InvalidSuccessResponseError reports a 2xx registration response that cannot
// be used as an OAuth client registration.
type InvalidSuccessResponseError struct {
	// Err explains why the successful response cannot be used.
	Err error

	// StatusCode is the successful response status, or zero when unavailable.
	StatusCode int
}

func (e *InvalidSuccessResponseError) Error() string {
	return fmt.Sprintf("invalid dynamic client registration response: %v", e.Err)
}

func (e *InvalidSuccessResponseError) Unwrap() error { return e.Err }

// ClassifyDCR maps a Dynamic Client Registration failure onto the stable
// administrator-facing contract.
func ClassifyDCR(err error) Failure {
	if errors.Is(err, context.Canceled) {
		return Failure{Outcome: "", Reason: "", Retryable: false, HTTPStatus: nil, ProviderMessage: nil}
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		status := httpErr.StatusCode
		failure := Failure{
			Outcome:         OutcomeRefused,
			Reason:          ReasonAuthorizationRejected,
			Retryable:       false,
			HTTPStatus:      &status,
			ProviderMessage: optionalProviderMessage(httpErr.ProviderMessage),
		}
		switch {
		case status == http.StatusRequestTimeout:
			failure.Outcome = OutcomeUnreachable
			failure.Reason = ReasonTimeout
			failure.Retryable = true
		case status == http.StatusTooManyRequests:
			failure.Outcome = OutcomeUnreachable
			failure.Reason = ReasonRateLimited
			failure.Retryable = true
		case status >= http.StatusInternalServerError:
			failure.Outcome = OutcomeUnreachable
			failure.Reason = ReasonUpstreamUnavailable
			failure.Retryable = true
		}
		return failure
	}

	var invalidResponse *InvalidSuccessResponseError
	if errors.As(err, &invalidResponse) {
		return InvalidSuccessResponse(invalidResponse.StatusCode)
	}

	failure := Failure{
		Outcome:         OutcomeUnreachable,
		Reason:          ReasonNetworkError,
		Retryable:       true,
		HTTPStatus:      nil,
		ProviderMessage: nil,
	}
	if errors.Is(err, context.DeadlineExceeded) {
		failure.Reason = ReasonTimeout
		return failure
	}
	if errors.Is(err, guardian.ErrRateLimited) {
		failure.Reason = ReasonRateLimited
		return failure
	}
	if errors.Is(err, guardian.ErrCircuitOpen) {
		failure.Reason = ReasonUpstreamUnavailable
		return failure
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		failure.Reason = ReasonDNSError
		return failure
	}
	if isTLSError(err) {
		failure.Reason = ReasonTLSError
		return failure
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		failure.Reason = ReasonTimeout
		return failure
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		failure.Reason = ReasonNetworkError
	}
	return failure
}

// InvalidSuccessResponse returns the non-retryable result used when a provider
// answers successfully but omits or contradicts required client metadata.
func InvalidSuccessResponse(statusCode int) Failure {
	failure := Failure{
		Outcome:         OutcomeRefused,
		Reason:          ReasonInvalidSuccessResponse,
		Retryable:       false,
		HTTPStatus:      nil,
		ProviderMessage: nil,
	}
	if statusCode != 0 {
		failure.HTTPStatus = &statusCode
	}
	return failure
}

// ClassifyCIMDAuthorizationError classifies a provider error returned to the
// OAuth callback. Ordinary user cancellation is not a registration failure.
func ClassifyCIMDAuthorizationError(code, description string) (Failure, bool) {
	if code == "" || code == "access_denied" {
		return Failure{Outcome: "", Reason: "", Retryable: false, HTTPStatus: nil, ProviderMessage: nil}, false
	}
	failure := Failure{
		Outcome:         OutcomeRefused,
		Reason:          ReasonAuthorizationRejected,
		Retryable:       false,
		HTTPStatus:      nil,
		ProviderMessage: optionalProviderMessage(description),
	}
	if code == "server_error" || code == "temporarily_unavailable" {
		failure.Outcome = OutcomeUnreachable
		failure.Reason = ReasonUpstreamUnavailable
		failure.Retryable = true
	}
	return failure, true
}

// SanitizeProviderMessage normalizes provider-controlled text to one line,
// removes control and formatting characters, and truncates at a rune boundary.
func SanitizeProviderMessage(message string) string {
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, message)
	return conv.TruncateString(strings.Join(strings.Fields(message), " "), ProviderMessageMaxRunes)
}

func optionalProviderMessage(message string) *string {
	message = SanitizeProviderMessage(message)
	return conv.PtrEmpty(message)
}

func isTLSError(err error) bool {
	var certificateVerificationErr *tls.CertificateVerificationError
	var recordHeaderErr tls.RecordHeaderError
	var unknownAuthorityErr x509.UnknownAuthorityError
	var certificateInvalidErr x509.CertificateInvalidError
	var hostnameErr x509.HostnameError
	var systemRootsErr x509.SystemRootsError
	return errors.As(err, &certificateVerificationErr) ||
		errors.As(err, &recordHeaderErr) ||
		errors.As(err, &unknownAuthorityErr) ||
		errors.As(err, &certificateInvalidErr) ||
		errors.As(err, &hostnameErr) ||
		errors.As(err, &systemRootsErr)
}
