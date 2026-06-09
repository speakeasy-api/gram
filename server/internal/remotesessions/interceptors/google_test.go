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

func TestGoogleMatchesGoogleIssuerCaseInsensitively(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	require.True(t, ic.Match("https://Accounts.Google.com"))
	require.True(t, ic.Match("https://ACCOUNTS.GOOGLE.COM/o/oauth2/v2/auth"))
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

func TestGoogleModifyAuthorizePreservesExistingPrompt(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	q := url.Values{}
	q.Set("prompt", "select_account")
	ic.ModifyAuthorize(t.Context(), q)

	require.Equal(t, "offline", q.Get("access_type"))
	require.Equal(t, "select_account consent", q.Get("prompt"))
}

func TestGoogleModifyAuthorizeDoesNotDuplicateConsent(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	q := url.Values{}
	q.Set("prompt", "consent select_account")
	ic.ModifyAuthorize(t.Context(), q)

	require.Equal(t, "consent select_account", q.Get("prompt"))
}

func TestGoogleName(t *testing.T) {
	t.Parallel()

	ic := interceptors.NewGoogle(testenv.NewLogger(t))
	require.Equal(t, "google-offline-access", ic.Name())
}
