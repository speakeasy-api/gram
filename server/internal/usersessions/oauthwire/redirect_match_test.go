package oauthwire_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

func TestRedirectURIMatchesExactEntry(t *testing.T) {
	t.Parallel()

	registered := []string{"https://app.example.com/callback", "http://localhost:4321/callback"}

	require.True(t, oauthwire.RedirectURIMatches(registered, "https://app.example.com/callback", false))
	require.True(t, oauthwire.RedirectURIMatches(registered, "http://localhost:4321/callback", false))
	require.False(t, oauthwire.RedirectURIMatches(registered, "https://app.example.com/other", false))
	require.False(t, oauthwire.RedirectURIMatches(nil, "https://app.example.com/callback", true))
}

func TestRedirectURIMatchesVariableLoopbackPort(t *testing.T) {
	t.Parallel()

	registered := []string{"http://localhost:4321/callback"}

	require.True(t, oauthwire.RedirectURIMatches(registered, "http://localhost:59122/callback", true),
		"RFC 8252 §7.3 requires any loopback port to be accepted")
	require.True(t, oauthwire.RedirectURIMatches([]string{"http://127.0.0.1:4321/callback"}, "http://127.0.0.1:1/callback", true))
	require.True(t, oauthwire.RedirectURIMatches([]string{"http://[::1]:4321/callback"}, "http://[::1]:9999/callback", true))
	require.True(t, oauthwire.RedirectURIMatches([]string{"http://LOCALHOST:4321/callback"}, "http://localhost:59122/callback", true),
		"host comparison is case-insensitive")
}

func TestRedirectURIMatchesWithoutLoopbackVariance(t *testing.T) {
	t.Parallel()

	require.False(t, oauthwire.RedirectURIMatches([]string{"http://localhost:4321/callback"}, "http://localhost:59122/callback", false),
		"clients that did not earn the exception keep byte-exact matching")
}

func TestRedirectURIMatchesOnlyVariesThePort(t *testing.T) {
	t.Parallel()

	registered := []string{"http://localhost:4321/callback"}

	require.False(t, oauthwire.RedirectURIMatches(registered, "http://localhost:59122/callback/extra", true),
		"path must match")
	require.False(t, oauthwire.RedirectURIMatches(registered, "http://localhost:59122/callback?next=https://evil.example", true),
		"query must match")
	require.False(t, oauthwire.RedirectURIMatches(registered, "http://localhost:59122/%63allback", true),
		"an encoding variant of the path must not match")
	require.False(t, oauthwire.RedirectURIMatches(registered, "http://user:pass@localhost:59122/callback", true),
		"userinfo must disqualify the request")
	require.False(t, oauthwire.RedirectURIMatches([]string{"http://user:pass@localhost:4321/callback"}, "http://localhost:59122/callback", true),
		"userinfo must disqualify the registered entry")
	require.False(t, oauthwire.RedirectURIMatches(registered, "http://localhost:59122/callback#", true),
		"a fragment must disqualify the request")
	require.False(t, oauthwire.RedirectURIMatches([]string{"http://localhost:4321/callback#"}, "http://localhost:59122/callback", true),
		"a fragment must disqualify the registered entry")
	require.False(t, oauthwire.RedirectURIMatches(registered, "https://localhost:59122/callback", true),
		"scheme must match")
	require.False(t, oauthwire.RedirectURIMatches([]string{"https://app.example.com/callback"}, "https://app.example.com:8443/callback", true),
		"the exception is loopback-only")
}
