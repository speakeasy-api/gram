package mockoidc_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	mockoidc "github.com/speakeasy-api/gram/mock-oidc"
)

func newRedirectConfig(clients ...mockoidc.OAuthClient) *mockoidc.Config {
	return &mockoidc.Config{
		Provider: mockoidc.ProviderConfig{
			Users:        []mockoidc.User{{Email: "eng@speakeasyapi.dev"}},
			OAuthClients: clients,
		},
	}
}

func TestAddRedirectURIsAppendsToEveryClient(t *testing.T) {
	t.Parallel()

	cfg := newRedirectConfig(
		mockoidc.OAuthClient{ClientID: "a", RedirectURIs: []string{"https://localhost:8083/admin/auth.callback"}},
		mockoidc.OAuthClient{ClientID: "b", RedirectURIs: []string{"https://localhost:8083/admin/auth.callback"}},
	)

	cfg.AddRedirectURIs("https://localhost:41234/admin/auth.callback")

	for _, id := range []string{"a", "b"} {
		client, ok := cfg.FindClient(id)
		require.True(t, ok, "client %s should exist", id)
		require.Equal(t, []string{
			"https://localhost:8083/admin/auth.callback",
			"https://localhost:41234/admin/auth.callback",
		}, client.RedirectURIs)
	}
}

func TestAddRedirectURIsSkipsDuplicates(t *testing.T) {
	t.Parallel()

	cfg := newRedirectConfig(
		mockoidc.OAuthClient{ClientID: "a", RedirectURIs: []string{"https://localhost:8083/admin/auth.callback"}},
	)

	// Same URI twice, and one that already exists, should not produce duplicates.
	cfg.AddRedirectURIs(
		"https://localhost:8083/admin/auth.callback",
		"https://localhost:41234/admin/auth.callback",
		"https://localhost:41234/admin/auth.callback",
	)

	client, ok := cfg.FindClient("a")
	require.True(t, ok)
	require.Equal(t, []string{
		"https://localhost:8083/admin/auth.callback",
		"https://localhost:41234/admin/auth.callback",
	}, client.RedirectURIs)
}

func TestAddRedirectURIsIgnoresEmptyAndNoArgs(t *testing.T) {
	t.Parallel()

	cfg := newRedirectConfig(
		mockoidc.OAuthClient{ClientID: "a", RedirectURIs: []string{"https://localhost:8083/admin/auth.callback"}},
	)

	cfg.AddRedirectURIs()
	cfg.AddRedirectURIs("")

	client, ok := cfg.FindClient("a")
	require.True(t, ok)
	require.Equal(t, []string{"https://localhost:8083/admin/auth.callback"}, client.RedirectURIs)
}
