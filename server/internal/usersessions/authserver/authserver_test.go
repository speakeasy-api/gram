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
	testPlatformHostBaseURL       = "https://mcp.example.com"
)

// testHosts is a deployment with an authentication host and one extra
// platform host.
var testHosts = authserver.Hosts{
	ServerURL:                 testServerURL,
	AuthenticationHostBaseURL: testAuthenticationHostBaseURL,
	PlatformHosts:             map[string]string{"mcp.example.com": testPlatformHostBaseURL},
}

func sharedIssuer(useAuthenticationHost bool) repo.UserSessionIssuer {
	return repo.UserSessionIssuer{
		ID:                      uuid.New(),
		AuthorizationServerMode: string(authserver.ModeShared),
		UseAuthenticationHost:   useAuthenticationHost,
	}
}

// pinnedIssuer is an issuer in shared mode pinned to its own shared
// authorization server path on origin.
func pinnedIssuer(origin string) repo.UserSessionIssuer {
	issuer := sharedIssuer(false)
	issuer.PinnedIssuerUrl = conv.ToPGText(origin + authserver.SharedPath(issuer.ID))
	return issuer
}

func TestHosts_SharedIssuerURLOnAuthenticationHost(t *testing.T) {
	t.Parallel()

	pinned := pinnedIssuer(testAuthenticationHostBaseURL)
	issuerURL, err := testHosts.SharedIssuerURL(pinned)
	require.NoError(t, err)
	require.Equal(t, testAuthenticationHostBaseURL+authserver.SharedPath(pinned.ID), issuerURL)

	optedIn := sharedIssuer(true)
	issuerURL, err = testHosts.SharedIssuerURL(optedIn)
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

	_, err := hosts.SharedIssuerURL(pinnedIssuer(testAuthenticationHostBaseURL))
	require.ErrorIs(t, err, authserver.ErrHostNotServed)

	optedIn := sharedIssuer(true)
	issuerURL, err := hosts.SharedIssuerURL(optedIn)
	require.NoError(t, err)
	require.Equal(t, testServerURL+authserver.SharedPath(optedIn.ID), issuerURL)
}

func TestHosts_SharedIssuerURLOnPlatformHost(t *testing.T) {
	t.Parallel()

	pinned := pinnedIssuer(testPlatformHostBaseURL)
	issuerURL, err := testHosts.SharedIssuerURL(pinned)
	require.NoError(t, err)
	require.Equal(t, testPlatformHostBaseURL+authserver.SharedPath(pinned.ID), issuerURL)
}

func TestHosts_SharedIssuerURLRefusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		issuer func() repo.UserSessionIssuer
		err    error
	}{
		{name: "endpoint mode", err: authserver.ErrNotShared, issuer: func() repo.UserSessionIssuer {
			issuer := sharedIssuer(false)
			issuer.AuthorizationServerMode = string(authserver.ModeEndpoint)
			return issuer
		}},
		{name: "another issuer's path", err: authserver.ErrPinnedIssuerURLInvalid, issuer: func() repo.UserSessionIssuer {
			issuer := sharedIssuer(false)
			issuer.PinnedIssuerUrl = conv.ToPGText(testServerURL + authserver.SharedPath(uuid.New()))
			return issuer
		}},
		{name: "query", err: authserver.ErrPinnedIssuerURLInvalid, issuer: func() repo.UserSessionIssuer {
			issuer := pinnedIssuer(testServerURL)
			issuer.PinnedIssuerUrl = conv.ToPGText(issuer.PinnedIssuerUrl.String + "?x=1")
			return issuer
		}},
		{name: "unserved host", err: authserver.ErrHostNotServed, issuer: func() repo.UserSessionIssuer {
			return pinnedIssuer("https://other.example.com")
		}},
		{name: "no origin", err: authserver.ErrPinnedIssuerURLInvalid, issuer: func() repo.UserSessionIssuer {
			return pinnedIssuer("")
		}},
		{name: "unparseable", err: authserver.ErrPinnedIssuerURLInvalid, issuer: func() repo.UserSessionIssuer {
			issuer := sharedIssuer(false)
			issuer.PinnedIssuerUrl = conv.ToPGText("https://app.example.com/%zz")
			return issuer
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := testHosts.SharedIssuerURL(tc.issuer())
			require.ErrorIs(t, err, tc.err)

			_, err = testHosts.SharedAuthorizationServer(tc.issuer())
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestHosts_SharedAuthorizationServer(t *testing.T) {
	t.Parallel()

	pinned := pinnedIssuer(testServerURL)
	server, err := testHosts.SharedAuthorizationServer(pinned)
	require.NoError(t, err)
	require.Equal(t, testServerURL+authserver.SharedPath(pinned.ID), server.IssuerURL)
	require.Equal(t, testServerURL+authserver.SharedPath(pinned.ID)+"/token", server.TokenEndpoint)
}

func TestHosts_SharedIssuerURLOn(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	require.Equal(t, testAuthenticationHostBaseURL+authserver.SharedPath(id), testHosts.SharedIssuerURLOn(id, true))
	require.Equal(t, testServerURL+authserver.SharedPath(id), testHosts.SharedIssuerURLOn(id, false))

	withoutAuthenticationHost := authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: "", PlatformHosts: nil}
	require.False(t, withoutAuthenticationHost.AuthenticationHostAvailable())
	require.Equal(t, testServerURL+authserver.SharedPath(id), withoutAuthenticationHost.SharedIssuerURLOn(id, true))
}

func TestHosts_PerServerOrigin(t *testing.T) {
	t.Parallel()

	require.True(t, testHosts.AuthenticationHostAvailable())
	require.Equal(t, testAuthenticationHostBaseURL, testHosts.PerServerOrigin(true))
	require.Equal(t, testServerURL, testHosts.PerServerOrigin(false))

	withoutAuthenticationHost := authserver.Hosts{ServerURL: testServerURL, AuthenticationHostBaseURL: "", PlatformHosts: nil}
	require.Equal(t, testServerURL, withoutAuthenticationHost.PerServerOrigin(true))
}

func TestIssuerURLPinned(t *testing.T) {
	t.Parallel()

	require.True(t, authserver.IssuerURLPinned(pinnedIssuer(testServerURL)))
	require.False(t, authserver.IssuerURLPinned(sharedIssuer(false)))

	empty := sharedIssuer(false)
	empty.PinnedIssuerUrl = conv.ToPGText("")
	require.False(t, authserver.IssuerURLPinned(empty))

	endpoint := pinnedIssuer(testServerURL)
	endpoint.AuthorizationServerMode = string(authserver.ModeEndpoint)
	require.False(t, authserver.IssuerURLPinned(endpoint))
}

func TestHosts_ServesSharedOn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		origin string
		served bool
	}{
		{name: "server url", origin: testServerURL, served: true},
		{name: "authentication host", origin: testAuthenticationHostBaseURL, served: true},
		{name: "platform host", origin: testPlatformHostBaseURL, served: true},
		{name: "authentication host over http", origin: "http://id.example.com", served: false},
		{name: "other host", origin: "https://other.example.com", served: false},
		{name: "empty", origin: "", served: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.served, testHosts.ServesSharedOn(tc.origin))
		})
	}
}
