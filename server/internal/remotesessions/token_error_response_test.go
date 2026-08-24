package remotesessions

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTokenErrorResponseRFC6749(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusBadRequest, []byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "Unknown or invalid refresh token.", got.ErrorDescription)
	require.Equal(t, "invalid_grant: Unknown or invalid refresh token.", got.summary("400 Bad Request"))
}

func TestParseTokenErrorResponseRFC6749CodeOnly(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusServiceUnavailable, []byte(`{"error":"temporarily_unavailable"}`))
	require.Equal(t, "temporarily_unavailable", got.Error)
	require.Empty(t, got.ErrorDescription)
	require.Equal(t, "temporarily_unavailable", got.summary("503 Service Unavailable"))
}

func TestParseTokenErrorResponseDatadogErrorsArray(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusBadRequest, []byte(`{"errors": ["invalid_grant - Invalid or expired refresh token or code verifier."]}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "Invalid or expired refresh token or code verifier.", got.ErrorDescription)
	require.Equal(t, "invalid_grant: Invalid or expired refresh token or code verifier.", got.summary("400 Bad Request"))
}

func TestParseTokenErrorResponseDubNestedUnauthorized(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusUnauthorized, []byte(`{"error":{"code":"unauthorized","message":"Refresh token not found."}}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "Refresh token not found.", got.ErrorDescription)
	require.Equal(t, "invalid_grant: Refresh token not found.", got.summary("401 Unauthorized"))
}

func TestParseTokenErrorResponseDubNestedInvalidGrant(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusUnauthorized, []byte(`{"error":{"code":"invalid_grant","message":"The refresh token is expired."}}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "The refresh token is expired.", got.ErrorDescription)
}

func TestParseTokenErrorResponseDoesNotRemapOtherUnauthorized(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusUnauthorized, []byte(`{"error":{"code":"unauthorized","message":"Invalid client credentials."}}`))
	require.Equal(t, "unauthorized", got.Error)
	require.Equal(t, "Invalid client credentials.", got.ErrorDescription)
	require.NotEqual(t, oauthErrInvalidGrant, got.Error)
}

func TestParseTokenErrorResponseDoesNotRemapClientAuthFailureMentioningRefreshToken(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusUnauthorized, []byte(`{"error":{"code":"unauthorized","message":"Invalid client credentials for refresh token exchange."}}`))
	require.Equal(t, "unauthorized", got.Error)
	require.Equal(t, "Invalid client credentials for refresh token exchange.", got.ErrorDescription)
}

func TestParseTokenErrorResponseDoesNotRemapClientSubjectFailures(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusUnauthorized, []byte(`{"error":{"code":"unauthorized","message":"client ID is invalid for refresh token exchange"}}`))
	require.Equal(t, "unauthorized", got.Error)

	got = parseTokenErrorResponse(http.StatusUnauthorized, []byte(`{"error":{"code":"unauthorized","message":"client is unauthorized"}}`))
	require.Equal(t, "unauthorized", got.Error)
}

func TestParseTokenErrorResponseRemapsDeadGrantNamingAClient(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusUnauthorized, []byte(`{"error":{"code":"unauthorized","message":"Refresh token not found for client abc."}}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "Refresh token not found for client abc.", got.ErrorDescription)
}

func TestParseTokenErrorResponseDoesNotOverrideOtherRFCCodes(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusBadRequest, []byte(`{"error":"invalid_client","error_description":"do not mention invalid_grant here"}`))
	require.Equal(t, "invalid_client", got.Error)
	require.Equal(t, "do not mention invalid_grant here", got.ErrorDescription)
}

func TestParseTokenErrorResponseEmptyBody(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusBadRequest, nil)
	require.Empty(t, got.Error)
	require.Equal(t, "HTTP 400 Bad Request", got.summary("400 Bad Request"))
}

func TestParseTokenErrorResponseKeepsRFCMembersNextToMalformedExtension(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusBadRequest, []byte(`{"error":"invalid_grant","error_description":"Token has been revoked.","errors":[{"detail":"x"}]}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "Token has been revoked.", got.ErrorDescription)
}

func TestParseTokenErrorResponseStandaloneMentionDropsUpstreamMessage(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(http.StatusBadRequest, []byte(`{"errors":["Grant failure: invalid_grant (see docs)"]}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Empty(t, got.ErrorDescription)
	require.Equal(t, oauthErrInvalidGrant, got.summary("400 Bad Request"))
}

func TestNewTokenRefreshErrorFromHTTPDatadogInvalidGrant(t *testing.T) {
	t.Parallel()

	err := newTokenRefreshErrorFromHTTP(
		http.StatusBadRequest,
		"400 Bad Request",
		[]byte(`{"errors": ["invalid_grant - Invalid or expired refresh token or code verifier."]}`),
	)
	require.True(t, err.invalidGrant())
	require.Equal(t, "invalid_grant: Invalid or expired refresh token or code verifier.", err.Reason)
}

func TestNewTokenRefreshErrorFromHTTPDubUnauthorizedInvalidGrant(t *testing.T) {
	t.Parallel()

	err := newTokenRefreshErrorFromHTTP(
		http.StatusUnauthorized,
		"401 Unauthorized",
		[]byte(`{"error":{"code":"unauthorized","message":"Refresh token not found."}}`),
	)
	require.True(t, err.invalidGrant())
	require.Equal(t, "invalid_grant: Refresh token not found.", err.Reason)
}

func TestNewTokenRefreshErrorFromHTTPGeneric4xxInvalidGrantToken(t *testing.T) {
	t.Parallel()

	err := newTokenRefreshErrorFromHTTP(
		http.StatusBadRequest,
		"400 Bad Request",
		[]byte(`token endpoint rejected refresh: invalid_grant`),
	)
	require.True(t, err.invalidGrant())
	require.Equal(t, oauthErrInvalidGrant, err.Reason)
}

func TestNewTokenRefreshErrorFromHTTPServerErrorDoesNotTreatInvalidGrantToken(t *testing.T) {
	t.Parallel()

	err := newTokenRefreshErrorFromHTTP(
		http.StatusInternalServerError,
		"500 Internal Server Error",
		[]byte(`upstream mentioned invalid_grant on an error page`),
	)
	require.False(t, err.invalidGrant())
	require.Equal(t, "HTTP 500 Internal Server Error", err.Reason)
}

func TestNewTokenRefreshErrorFromHTTPServerErrorJSONMentionDoesNotClearGrant(t *testing.T) {
	t.Parallel()

	err := newTokenRefreshErrorFromHTTP(
		http.StatusInternalServerError,
		"500 Internal Server Error",
		[]byte(`{"error":"token rejected: invalid_grant"}`),
	)
	require.False(t, err.invalidGrant())
}

func TestNewTokenRefreshErrorFromHTTPRateLimitMentionDoesNotClearGrant(t *testing.T) {
	t.Parallel()

	err := newTokenRefreshErrorFromHTTP(
		http.StatusTooManyRequests,
		"429 Too Many Requests",
		[]byte(`{"message":"Too many requests; see https://api.example.com/docs/errors#invalid_grant"}`),
	)
	require.False(t, err.invalidGrant())
	require.True(t, IsTokenRefreshRateLimited(err))
}

func TestNewTokenRefreshErrorFromHTTPServerErrorExactRFCCodeStillDefinitive(t *testing.T) {
	t.Parallel()

	err := newTokenRefreshErrorFromHTTP(
		http.StatusInternalServerError,
		"500 Internal Server Error",
		[]byte(`{"error":"invalid_grant"}`),
	)
	require.True(t, err.invalidGrant())
}
