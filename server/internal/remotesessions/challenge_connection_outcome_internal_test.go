package remotesessions

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

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

	failure := exchangeFailure(t.Context(), testenv.NewLogger(t), err, false)
	require.Equal(t, oops.CodeGatewayError, failure.Code, "the status is unchanged")
	require.Equal(t, "token exchange with the identity provider failed: the identity provider's address is not allowed by network policy", failure.Error())
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
