package remotesessions

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestExchangeFailure_FixedReasons(t *testing.T) {
	t.Parallel()

	gatewayFailure := newTokenEndpointError(http.StatusBadGateway, "502 Bad Gateway", nil)
	gatewayFailure.tunnelError = wire.TunnelErrorSubstreamFailed

	cases := []struct {
		name     string
		err      error
		tunneled bool
		want     string
		wantCode oops.Code
	}{
		{
			name:     "tunnel gateway failure",
			err:      gatewayFailure,
			tunneled: true,
			want:     "token exchange with the identity provider failed: the MCP tunnel could not reach the identity provider",
			wantCode: oops.CodeGatewayError,
		},
		{
			name:     "tunnel header from a direct provider",
			err:      gatewayFailure,
			tunneled: false,
			want:     "token exchange with the identity provider failed: HTTP 502",
			wantCode: oops.CodeGatewayError,
		},
		{
			name:     "bare 502 on a tunneled issuer",
			err:      newTokenEndpointError(http.StatusBadGateway, "502 Bad Gateway", nil),
			tunneled: true,
			want:     "token exchange with the identity provider failed: HTTP 502 from the identity provider or the MCP tunnel agent",
			wantCode: oops.CodeGatewayError,
		},
		{
			name:     "provider 400 on a tunneled issuer",
			err:      newTokenEndpointError(http.StatusBadRequest, "400 Bad Request", []byte(`{"error":"app_not_enabled"}`)),
			tunneled: true,
			want:     "token exchange with the identity provider failed: HTTP 400",
			wantCode: oops.CodeUnauthorized,
		},
		{
			name:     "no tunnel route",
			err:      &tokenExchangeUnavailableError{err: fmt.Errorf("post token: %w", tunnelrouting.ErrNoTunnelRoute)},
			tunneled: true,
			want:     "token exchange with the identity provider failed: the MCP tunnel is not connected",
			wantCode: oops.CodeGatewayError,
		},
		{
			name:     "provider unreachable",
			err:      &tokenExchangeUnavailableError{err: errors.New("post token: dial tcp: connection refused")},
			tunneled: false,
			want:     "token exchange with the identity provider failed: the identity provider could not be reached",
			wantCode: oops.CodeGatewayError,
		},
		{
			name:     "client assertion signing failed",
			err:      fmt.Errorf("new token request: %w", &tokenEndpointSigningError{err: errors.New("kms detail")}),
			tunneled: false,
			want:     "token exchange with the identity provider failed: the client assertion could not be signed",
			wantCode: oops.CodeUnexpected,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			shareable := exchangeFailure(t.Context(), testenv.NewLogger(t), tc.err, tc.tunneled)
			require.Equal(t, tc.want, shareable.Error())
			require.Equal(t, tc.wantCode, shareable.Code)
		})
	}
}
