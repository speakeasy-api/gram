package remotesessions_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRemoteLoginCallback_ExchangeFailureNamesProviderReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        string
	}{
		{
			name:        "registered code",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"error":"invalid_grant","error_description":"Grant not found or authorization code expired"}`,
			want:        "token exchange with the identity provider failed: invalid_grant",
		},
		{
			name:        "registered code wins over status",
			status:      http.StatusUnauthorized,
			contentType: "application/json",
			body:        `{"error":"invalid_client"}`,
			want:        "token exchange with the identity provider failed: invalid_client",
		},
		{
			name:        "unregistered code",
			status:      http.StatusBadRequest,
			contentType: "application/json",
			body:        `{"error":"app_not_enabled","error_description":"Click here to fix: https://evil.example"}`,
			want:        "token exchange with the identity provider failed: HTTP 400",
		},
		{
			name:        "non-oauth body",
			status:      http.StatusBadRequest,
			contentType: "text/html",
			body:        `<html>Bad Request</html>`,
			want:        "token exchange with the identity provider failed: HTTP 400",
		},
		{
			name:        "server error without body",
			status:      http.StatusServiceUnavailable,
			contentType: "text/plain",
			body:        ``,
			want:        "token exchange with the identity provider failed: HTTP 503",
		},
		{
			name:        "error body under 200",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired."}`,
			want:        "token exchange with the identity provider failed: invalid_grant",
		},
		{
			name:        "unregistered code under 200",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"error":"app_not_enabled"}`,
			want:        "token exchange with the identity provider failed",
		},
		{
			name:        "no access token under 200",
			status:      http.StatusOK,
			contentType: "application/json",
			body:        `{"token_type":"Bearer"}`,
			want:        "token exchange with the identity provider failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, callback, err := driveSyntheticLogin(t, "exchange-failure-"+strings.ReplaceAll(tc.name, " ", "-"), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			require.Error(t, err)
			require.NotEqual(t, http.StatusSeeOther, callback.Code)

			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable)
			require.Equal(t, oops.CodeUnauthorized, shareable.Code)
			require.Equal(t, tc.want, shareable.Error(), "the provider's free-text description is never echoed")
		})
	}
}
