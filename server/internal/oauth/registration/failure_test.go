package registration

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

func TestClassifyDCRHTTPFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status    int
		outcome   Outcome
		reason    Reason
		retryable bool
	}{
		{status: http.StatusBadRequest, outcome: OutcomeRefused, reason: ReasonAuthorizationRejected, retryable: false},
		{status: http.StatusConflict, outcome: OutcomeRefused, reason: ReasonAuthorizationRejected, retryable: false},
		{status: http.StatusRequestTimeout, outcome: OutcomeUnreachable, reason: ReasonTimeout, retryable: true},
		{status: http.StatusTooManyRequests, outcome: OutcomeUnreachable, reason: ReasonRateLimited, retryable: true},
		{status: http.StatusServiceUnavailable, outcome: OutcomeUnreachable, reason: ReasonUpstreamUnavailable, retryable: true},
	}
	for _, tc := range cases {
		failure := ClassifyDCR(&HTTPError{StatusCode: tc.status, ProviderMessage: " rejected\nrequest "})
		require.Equal(t, tc.outcome, failure.Outcome, tc.status)
		require.Equal(t, tc.reason, failure.Reason, tc.status)
		require.Equal(t, tc.retryable, failure.Retryable, tc.status)
		require.NotNil(t, failure.HTTPStatus, tc.status)
		require.Equal(t, tc.status, *failure.HTTPStatus, tc.status)
		require.NotNil(t, failure.ProviderMessage, tc.status)
		require.Equal(t, "rejected request", *failure.ProviderMessage, tc.status)
	}
}

func TestClassifyDCRTransportAndInvalidResponseFailures(t *testing.T) {
	t.Parallel()

	dnsFailure := ClassifyDCR(&net.DNSError{Err: "no such host", Name: "provider.example"})
	require.Equal(t, Failure{Outcome: OutcomeUnreachable, Reason: ReasonDNSError, Retryable: true, HTTPStatus: nil, ProviderMessage: nil}, dnsFailure)

	tlsFailure := ClassifyDCR(x509.UnknownAuthorityError{})
	require.Equal(t, Failure{Outcome: OutcomeUnreachable, Reason: ReasonTLSError, Retryable: true, HTTPStatus: nil, ProviderMessage: nil}, tlsFailure)

	timeoutFailure := ClassifyDCR(context.DeadlineExceeded)
	require.Equal(t, Failure{Outcome: OutcomeUnreachable, Reason: ReasonTimeout, Retryable: true, HTTPStatus: nil, ProviderMessage: nil}, timeoutFailure)

	invalidFailure := ClassifyDCR(&InvalidSuccessResponseError{Err: errors.New("missing client_id"), StatusCode: http.StatusCreated})
	require.Equal(t, InvalidSuccessResponse(http.StatusCreated), invalidFailure)
}

func TestClassifyDCRCallerCancellationIsNotACompletedFailure(t *testing.T) {
	t.Parallel()

	require.Equal(t, Failure{Outcome: "", Reason: "", Retryable: false, HTTPStatus: nil, ProviderMessage: nil}, ClassifyDCR(context.Canceled))
}

func TestClassifyDCRGuardianResilienceFailures(t *testing.T) {
	t.Parallel()

	rateLimited := ClassifyDCR(&guardian.ResilienceError{Reason: guardian.ErrRateLimited, RetryAfter: 0})
	require.Equal(t, Failure{Outcome: OutcomeUnreachable, Reason: ReasonRateLimited, Retryable: true, HTTPStatus: nil, ProviderMessage: nil}, rateLimited)

	circuitOpen := ClassifyDCR(&guardian.ResilienceError{Reason: guardian.ErrCircuitOpen, RetryAfter: 0})
	require.Equal(t, Failure{Outcome: OutcomeUnreachable, Reason: ReasonUpstreamUnavailable, Retryable: true, HTTPStatus: nil, ProviderMessage: nil}, circuitOpen)
}

func TestSanitizeProviderMessage(t *testing.T) {
	t.Parallel()

	message := "  first\n\tsecond\x00 \u202esecret  " + strings.Repeat("x", ProviderMessageMaxRunes)
	got := SanitizeProviderMessage(message)

	require.NotContains(t, got, "\n")
	require.NotContains(t, got, "\x00")
	require.NotContains(t, got, "\u202e")
	require.Equal(t, ProviderMessageMaxRunes, utf8.RuneCountInString(got))
	require.True(t, strings.HasPrefix(got, "first second secret "))
}

func TestClassifyCIMDAuthorizationError(t *testing.T) {
	t.Parallel()

	refused, record := ClassifyCIMDAuthorizationError("unauthorized_client", " client rejected ")
	require.True(t, record)
	require.Equal(t, OutcomeRefused, refused.Outcome)
	require.Equal(t, ReasonAuthorizationRejected, refused.Reason)
	require.False(t, refused.Retryable)
	require.NotNil(t, refused.ProviderMessage)
	require.Equal(t, "client rejected", *refused.ProviderMessage)

	unavailable, record := ClassifyCIMDAuthorizationError("temporarily_unavailable", "try later")
	require.True(t, record)
	require.Equal(t, OutcomeUnreachable, unavailable.Outcome)
	require.Equal(t, ReasonUpstreamUnavailable, unavailable.Reason)
	require.True(t, unavailable.Retryable)

	_, record = ClassifyCIMDAuthorizationError("access_denied", "the user canceled")
	require.False(t, record)
}
