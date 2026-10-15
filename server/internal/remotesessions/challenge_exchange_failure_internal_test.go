package remotesessions

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestExchangeFailure_TunneledStatusIsAttributedToTheTunnel(t *testing.T) {
	t.Parallel()

	err := newTokenEndpointError(http.StatusBadGateway, "502 Bad Gateway", nil)
	shareable := exchangeFailure(t.Context(), testenv.NewLogger(t), err, true)
	require.Equal(t, "token exchange with the identity provider failed: HTTP 502 through the MCP tunnel", shareable.Error())
}
