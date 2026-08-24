package remotesessions

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTokenErrorResponseRFC6749(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse([]byte(`{"error":"invalid_grant","error_description":"Unknown or invalid refresh token."}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "Unknown or invalid refresh token.", got.ErrorDescription)
	require.Equal(t, "invalid_grant: Unknown or invalid refresh token.", got.summary("400 Bad Request"))
}

func TestParseTokenErrorResponseRFC6749CodeOnly(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse([]byte(`{"error":"temporarily_unavailable"}`))
	require.Equal(t, "temporarily_unavailable", got.Error)
	require.Empty(t, got.ErrorDescription)
	require.Equal(t, "temporarily_unavailable", got.summary("503 Service Unavailable"))
}

func TestParseTokenErrorResponseDatadogErrorsArray(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse([]byte(`{"errors": ["invalid_grant - Invalid or expired refresh token or code verifier."]}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "Invalid or expired refresh token or code verifier.", got.ErrorDescription)
	require.Equal(t, "invalid_grant: Invalid or expired refresh token or code verifier.", got.summary("400 Bad Request"))
}

func TestParseTokenErrorResponseDubNestedUnauthorized(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse([]byte(`{"error":{"code":"unauthorized","message":"Refresh token not found."}}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "Refresh token not found.", got.ErrorDescription)
	require.Equal(t, "invalid_grant: Refresh token not found.", got.summary("401 Unauthorized"))
}

func TestParseTokenErrorResponseDubNestedInvalidGrant(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse([]byte(`{"error":{"code":"invalid_grant","message":"The refresh token is expired."}}`))
	require.Equal(t, oauthErrInvalidGrant, got.Error)
	require.Equal(t, "The refresh token is expired.", got.ErrorDescription)
}

func TestParseTokenErrorResponseDoesNotRemapOtherUnauthorized(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse([]byte(`{"error":{"code":"unauthorized","message":"Invalid client credentials."}}`))
	require.Equal(t, "unauthorized", got.Error)
	require.Equal(t, "Invalid client credentials.", got.ErrorDescription)
	require.NotEqual(t, oauthErrInvalidGrant, got.Error)
}

func TestParseTokenErrorResponseDoesNotOverrideOtherRFCCodes(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse([]byte(`{"error":"invalid_client","error_description":"do not mention invalid_grant here"}`))
	require.Equal(t, "invalid_client", got.Error)
	require.Equal(t, "do not mention invalid_grant here", got.ErrorDescription)
}

func TestParseTokenErrorResponseEmptyBody(t *testing.T) {
	t.Parallel()

	got := parseTokenErrorResponse(nil)
	require.Empty(t, got.Error)
	require.Equal(t, "HTTP 400 Bad Request", got.summary("400 Bad Request"))
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
