// tokenservice_credentials_internal_test.go verifies that client credentials
// are form-urlencoded before entering the Basic authorization header (RFC
// 6749 §2.3.1). White-box so the shared request builder is exercised directly.

package remotesessions

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTokenEndpointRequest_BasicAuthEncodesCredentials(t *testing.T) {
	t.Parallel()

	req, err := newTokenEndpointRequest(
		t.Context(),
		"https://idp.example.com/token",
		url.Values{},
		TokenEndpointAuthMethodBasic,
		"ab+cd/ef=",
		"s+e/c==",
	)
	require.NoError(t, err)

	user, pass, ok := req.BasicAuth()
	require.True(t, ok)
	require.Equal(t, "ab%2Bcd%2Fef%3D", user, "client_id must be form-urlencoded per RFC 6749 §2.3.1")
	require.Equal(t, "s%2Be%2Fc%3D%3D", pass, "client_secret must be form-urlencoded per RFC 6749 §2.3.1")
}
