package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProviderErrorHappened(t *testing.T) {
	t.Parallel()

	for code, want := range map[string]mcpconnectionattempts.Code{
		oautherr.CodeInvalidScope:           mcpconnectionattempts.CodeProviderOAuthInvalidScope,
		oautherr.CodeUnauthorizedClient:     mcpconnectionattempts.CodeProviderOAuthInvalidScope,
		oautherr.CodeInvalidClient:          mcpconnectionattempts.CodeProviderOAuthInvalidClient,
		oautherr.CodeInvalidRequest:         mcpconnectionattempts.CodeSignInProviderError,
		oautherr.CodeTemporarilyUnavailable: mcpconnectionattempts.CodeProviderUnavailable,
		oautherr.CodeServerError:            mcpconnectionattempts.CodeProviderServerError,
		"something_else":                    mcpconnectionattempts.CodeSignInProviderError,
	} {
		require.Equal(t, want, providerErrorHappened(code), code)
	}
}

func TestExchangeRequestConfigurationIsNotProviderFailure(t *testing.T) {
	t.Parallel()
	manager := &ChallengeManager{}
	_, err := manager.exchangeCode(t.Context(), nil, RemoteLoginState{TokenEndpoint: "not-an-absolute-url"}, TokenEndpointClientAuth{Method: TokenEndpointAuthMethodNone}, "", "code")
	require.Error(t, err)
	require.Equal(t, mcpconnectionattempts.CodeCredentialMisconfigured, exchangeHappened(err, false))
}

// A token endpoint egress policy refuses is never dialed, so it is the admin's and says so.
func TestExchangeToBlockedAddressIsNotProviderFailure(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a blocked address must not be dialed")
	}))
	t.Cleanup(tokenServer.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{"127.0.0.0/8", "::1/128"})
	require.NoError(t, err)

	manager := &ChallengeManager{}
	_, err = manager.exchangeCode(t.Context(), policy.Client(), RemoteLoginState{TokenEndpoint: tokenServer.URL}, TokenEndpointClientAuth{Method: TokenEndpointAuthMethodNone}, "", "code")
	require.Error(t, err)
	require.Equal(t, mcpconnectionattempts.CodeAddressNotAllowed, exchangeHappened(err, false))

	page := httptest.NewRecorder()
	oops.BrowserErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
		return exchangeFailureAnswer(t.Context(), testenv.NewLogger(t), err, false, func() string { return "idp.example.com" })
	}).ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback", nil))
	text := strings.Join(strings.Fields(page.Body.String()), " ")
	require.Equal(t, http.StatusBadGateway, page.Code, "the status is unchanged")
	require.Contains(t, text, "because its sign-in provider uses a private or internal address Speakeasy can't reach")
	require.Contains(t, text, "Ask your IT administrators to check the addresses used by this server's sign-in provider")
	require.NotContains(t, text, "isn't available right now")
	require.NotContains(t, text, "idp.example.com isn't")

	failure := exchangeFailure(t.Context(), testenv.NewLogger(t), err, false)
	require.Equal(t, oops.CodeGatewayError, failure.Code, "the status is unchanged")
	require.Equal(t, "token exchange failed: the sign-in provider uses a private or internal address Speakeasy can't reach. Ask your administrator to check the sign-in provider's address", failure.Error())
}

// A lookup that fails for now, through the real resolver path, stays the provider's.
func TestExchangeWhoseLookupFailsStaysUnreachable(t *testing.T) {
	t.Parallel()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("resolver down")
	}})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithResolver(resolver))
	require.NoError(t, err)

	manager := &ChallengeManager{}
	_, err = manager.exchangeCode(t.Context(), policy.Client(), RemoteLoginState{TokenEndpoint: "https://token.example.invalid/token"}, TokenEndpointClientAuth{Method: TokenEndpointAuthMethodNone}, "", "code")
	require.Error(t, err)
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, exchangeHappened(err, false))
	require.Equal(t, "token exchange with the identity provider failed: the identity provider could not be reached", exchangeFailure(t.Context(), testenv.NewLogger(t), err, false).Error())
}

// A tunneled exchange dials our own gateway, so a refused address there is never the admin's.
func TestExchangeThroughATunnelIsNeverAnAddress(t *testing.T) {
	t.Parallel()

	err := &tokenExchangeUnavailableError{err: fmt.Errorf("post token: %w", guardian.ErrBlockedIP)}
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, exchangeHappened(err, true))
}

// The client credential branch: the endpoint keeps what it could not reach and whose issuer it is.
func TestClientCredentialOutageAtABlockedAddress(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a blocked address must not be dialed")
	}))
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{"127.0.0.0/8", "::1/128"})
	require.NoError(t, err)

	for _, tc := range []struct {
		name     string
		tunneled bool
		want     mcpconnectionattempts.Code
	}{
		{name: "direct", tunneled: false, want: mcpconnectionattempts.CodeAddressNotAllowed},
		{name: "tunneled", tunneled: true, want: mcpconnectionattempts.CodeRemoteSessionUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			endpoint := &TokenEndpoint{endpoint: server.URL, issuer: "https://idp.example.com", issuerID: uuid.New(), doer: policy.Client(), auth: TokenEndpointClientAuth{Method: TokenEndpointAuthMethodNone, ClientID: "client"}, tunneled: tc.tunneled}
			_, err := endpoint.Post(t.Context(), url.Values{"grant_type": {"client_credentials"}})
			require.Error(t, err)
			require.Equal(t, tc.want, ConnectionOutageHappened(err))
		})
	}
}

func TestExchangeHappened(t *testing.T) {
	t.Parallel()

	oauth := func(status int, code string) error {
		return &tokenEndpointError{statusCode: status, tunnelError: "", err: oautherr.RFC6749Error{Code: code}}
	}
	for name, tc := range map[string]struct {
		err      error
		tunneled bool
		want     mcpconnectionattempts.Code
	}{
		"invalid_client":  {oauth(http.StatusUnauthorized, oautherr.CodeInvalidClient), false, mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		"bare 401":        {oauth(http.StatusUnauthorized, ""), false, mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		"invalid_scope":   {oauth(http.StatusBadRequest, oautherr.CodeInvalidScope), false, mcpconnectionattempts.CodeProviderOAuthInvalidScope},
		"invalid_grant":   {oauth(http.StatusBadRequest, oautherr.CodeInvalidGrant), false, mcpconnectionattempts.CodeProviderOAuthInvalidGrant},
		"rate limited":    {oauth(http.StatusTooManyRequests, ""), false, mcpconnectionattempts.CodeProviderRateLimited},
		"invalid_request": {oauth(http.StatusBadRequest, oautherr.CodeInvalidRequest), false, mcpconnectionattempts.CodeSignInProviderError},
		"bare 400":        {oauth(http.StatusBadRequest, ""), false, mcpconnectionattempts.CodeSignInProviderError},
		"503":             {oauth(http.StatusServiceUnavailable, ""), false, mcpconnectionattempts.CodeProviderUnavailable},
		"500":             {oauth(http.StatusInternalServerError, ""), false, mcpconnectionattempts.CodeProviderServerError},
		"unreachable":     {&tokenExchangeUnavailableError{err: errors.New("dial")}, false, mcpconnectionattempts.CodeProviderUnreachable},
		"no such host":    {&tokenExchangeUnavailableError{err: &net.DNSError{Err: "no such host", Name: "idp.example.invalid", IsNotFound: true}}, false, mcpconnectionattempts.CodeAddressNotFound},
		"lookup timeout":  {&tokenExchangeUnavailableError{err: &net.DNSError{Err: "timeout", Name: "idp.example.com", IsTimeout: true, IsTemporary: true}}, false, mcpconnectionattempts.CodeProviderUnreachable},
		"no tunnel route": {&tokenExchangeUnavailableError{err: fmt.Errorf("post: %w", tunnelrouting.ErrNoTunnelRoute)}, true, mcpconnectionattempts.CodeTunnelUnavailable},
		"tunnel answered": {&tokenEndpointError{statusCode: http.StatusBadGateway, tunnelError: "unreachable", err: errors.New("x")}, true, mcpconnectionattempts.CodeTunnelUnavailable},
		"signing":         {&tokenEndpointSigningError{err: errors.New("kms")}, false, mcpconnectionattempts.CodeInternalError},
	} {
		require.Equal(t, tc.want, exchangeHappened(tc.err, tc.tunneled), name)
	}
}

func TestAuthorityHappened(t *testing.T) {
	t.Parallel()

	code, _ := AuthorityHappened(networkingress.ErrAuthorityUnavailable)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code, "an outage says nothing about the connection")
	code, _ = AuthorityHappened(errors.New("authority rejected"))
	require.Equal(t, mcpconnectionattempts.CodeConnectionChanged, code)
}

func TestTypedAuthorizationHappened(t *testing.T) {
	t.Parallel()
	require.Equal(t, mcpconnectionattempts.CodeNoServerAccess, TypedAuthorizationHappened(fmt.Errorf("binding revoked: %w", ErrTypedAuthorizationDenied)))
	require.Equal(t, mcpconnectionattempts.CodeInternalError, TypedAuthorizationHappened(errors.New("database unavailable")))
}

func TestTypedAuthorizationProvenance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want mcpconnectionattempts.Code
	}{
		{ErrTypedAuthorizationDenied, mcpconnectionattempts.CodeNoServerAccess},
		{ErrTypedAuthorizationChanged, mcpconnectionattempts.CodeConnectionChanged},
		{ErrTypedAuthorizationInvalid, mcpconnectionattempts.CodeInternalError},
		{ErrTypedAuthorizationConfiguration, mcpconnectionattempts.CodeConfigurationRequired},
	} {
		require.ErrorIs(t, tc.err, ErrTypedAuthorizationDenied)
		require.Equal(t, tc.want, TypedAuthorizationHappened(fmt.Errorf("validator: %w", tc.err)))
	}
}
