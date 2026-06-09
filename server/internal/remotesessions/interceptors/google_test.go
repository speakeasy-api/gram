package interceptors_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/interceptors"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestGoogleMatchesGoogleIssuer(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	require.True(t, ic.Match("https://accounts.google.com"))
	require.True(t, ic.Match("https://accounts.google.com/o/oauth2/v2/auth"))
}

func TestGoogleDoesNotMatchOtherIssuers(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	require.False(t, ic.Match("https://login.microsoftonline.com/common"))
	require.False(t, ic.Match("https://accounts.google.com.evil.example/auth"))
}

func TestGoogleDoesNotMatchMalformedIssuer(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	require.False(t, ic.Match("://not-a-url"))
}

func TestGoogleModifyAuthorizeRequestsOfflineAccess(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	q := url.Values{}
	ic.ModifyAuthorize(t.Context(), q)

	require.Equal(t, "offline", q.Get("access_type"))
	require.Equal(t, "consent", q.Get("prompt"))
}

func TestGoogleName(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	require.Equal(t, "google-offline-access", ic.Name())
}
