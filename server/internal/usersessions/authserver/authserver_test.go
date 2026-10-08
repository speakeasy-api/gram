package authserver_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	testServerURL                 = "https://app.example.com"
	testAuthenticationHostBaseURL = "https://id.example.com"
)

func sharedIssuer(pinnedIssuerURL string, useAuthenticationHost bool) repo.UserSessionIssuer {
	return repo.UserSessionIssuer{
		ID:                      uuid.New(),
		AuthorizationServerMode: string(authserver.ModeShared),
		PinnedIssuerUrl:         conv.ToPGTextEmpty(pinnedIssuerURL),
		UseAuthenticationHost:   useAuthenticationHost,
	}
}

func TestHosts_SharedIssuerURLOnAuthenticationHost(t *testing.T) {
	t.Parallel()

	hosts := authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: testAuthenticationHostBaseURL, PlatformHosts: nil}

	pinned := sharedIssuer("", false)
	pinned.PinnedIssuerUrl = conv.ToPGText(testAuthenticationHostBaseURL + authserver.SharedPath(pinned.ID))
	issuerURL, err := hosts.SharedIssuerURL(pinned)
	require.NoError(t, err)
	require.Equal(t, testAuthenticationHostBaseURL+authserver.SharedPath(pinned.ID), issuerURL)

	optedIn := sharedIssuer("", true)
	issuerURL, err = hosts.SharedIssuerURL(optedIn)
	require.NoError(t, err)
	require.Equal(t, testAuthenticationHostBaseURL+authserver.SharedPath(optedIn.ID), issuerURL)

	token, err := authserver.SharedTokenEndpoint(issuerURL)
	require.NoError(t, err)
	require.Equal(t, testAuthenticationHostBaseURL+authserver.SharedPath(optedIn.ID)+"/token", token)
}

// Without a configured authentication host, an issuer pinned to one names a
// server the deployment does not serve.
func TestHosts_SharedIssuerURLRefusesUnconfiguredAuthenticationHost(t *testing.T) {
	t.Parallel()

	hosts := authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: "", PlatformHosts: nil}

	pinned := sharedIssuer("", false)
	pinned.PinnedIssuerUrl = conv.ToPGText(testAuthenticationHostBaseURL + authserver.SharedPath(pinned.ID))
	_, err := hosts.SharedIssuerURL(pinned)
	require.Error(t, err)

	optedIn := sharedIssuer("", true)
	issuerURL, err := hosts.SharedIssuerURL(optedIn)
	require.NoError(t, err)
	require.Equal(t, testServerURL+authserver.SharedPath(optedIn.ID), issuerURL)
}

func TestHosts_ServesSharedOn(t *testing.T) {
	t.Parallel()

	hosts := authserver.Hosts{
		ServerURL:                 testServerURL,
		AuthenticationHostBaseURL: testAuthenticationHostBaseURL,
		PlatformHosts:             map[string]string{"mcp.example.com": "https://mcp.example.com"},
	}

	for _, tc := range []struct {
		name   string
		origin string
		served bool
	}{
		{name: "server url", origin: testServerURL, served: true},
		{name: "authentication host", origin: testAuthenticationHostBaseURL, served: true},
		{name: "platform host", origin: "https://mcp.example.com", served: true},
		{name: "authentication host over http", origin: "http://id.example.com", served: false},
		{name: "other host", origin: "https://other.example.com", served: false},
		{name: "empty", origin: "", served: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.served, hosts.ServesSharedOn(tc.origin))
		})
	}
}
