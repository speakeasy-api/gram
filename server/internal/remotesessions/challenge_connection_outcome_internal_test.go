package remotesessions

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

func TestProviderErrorHappened(t *testing.T) {
	t.Parallel()

	for code, want := range map[string]mcpconnectionattempts.Code{
		oautherr.CodeInvalidScope:           mcpconnectionattempts.CodeProviderOAuthInvalidScope,
		oautherr.CodeUnauthorizedClient:     mcpconnectionattempts.CodeProviderOAuthInvalidScope,
		oautherr.CodeInvalidClient:          mcpconnectionattempts.CodeProviderOAuthInvalidClient,
		oautherr.CodeTemporarilyUnavailable: mcpconnectionattempts.CodeProviderUnavailable,
		oautherr.CodeServerError:            mcpconnectionattempts.CodeProviderServerError,
		"something_else":                    mcpconnectionattempts.CodeProviderOAuthInvalidGrant,
	} {
		require.Equal(t, want, providerErrorHappened(code), code)
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
		"503":             {oauth(http.StatusServiceUnavailable, ""), false, mcpconnectionattempts.CodeProviderUnavailable},
		"500":             {oauth(http.StatusInternalServerError, ""), false, mcpconnectionattempts.CodeProviderServerError},
		"unreachable":     {&tokenExchangeUnavailableError{err: errors.New("dial")}, false, mcpconnectionattempts.CodeProviderUnreachable},
		"no tunnel route": {&tokenExchangeUnavailableError{err: fmt.Errorf("post: %w", tunnelrouting.ErrNoTunnelRoute)}, true, mcpconnectionattempts.CodeTunnelUnavailable},
		"tunnel answered": {&tokenEndpointError{statusCode: http.StatusBadGateway, tunnelError: "unreachable", err: errors.New("x")}, true, mcpconnectionattempts.CodeTunnelUnavailable},
		"signing":         {&tokenEndpointSigningError{err: errors.New("kms")}, false, mcpconnectionattempts.CodeInternalError},
	} {
		require.Equal(t, tc.want, exchangeHappened(tc.err, tc.tunneled), name)
	}
}
