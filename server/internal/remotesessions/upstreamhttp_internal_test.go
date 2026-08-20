package remotesessions

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/stretchr/testify/require"
)

func TestUpstreamHTTPDoerPublicIssuerDialsDirect(t *testing.T) {
	t.Parallel()

	direct := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).PooledClient()
	doer, err := upstreamHTTPDoer(direct, nil, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	require.NoError(t, err)
	require.Equal(t, httpDoer(direct), doer)
}

func TestUpstreamHTTPDoerTunnelBindingWins(t *testing.T) {
	t.Parallel()

	direct := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).PooledClient()
	tunnels := tunnelrouting.NewHTTPClient(route.NewRouteTable(), "forward-token", guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)), nil)
	tunnelID := uuid.New()

	doer, err := upstreamHTTPDoer(direct, tunnels, uuid.NullUUID{UUID: tunnelID, Valid: true})
	require.NoError(t, err)
	bound, ok := doer.(tunnelDoer)
	require.True(t, ok)
	require.Equal(t, tunnelID.String(), bound.tunnelID)
}

func TestUpstreamHTTPDoerTunnelBindingWithoutTransportErrors(t *testing.T) {
	t.Parallel()

	direct := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)).PooledClient()
	doer, err := upstreamHTTPDoer(direct, nil, uuid.NullUUID{UUID: uuid.New(), Valid: true})
	require.Error(t, err)
	require.Nil(t, doer)
}

var _ httpDoer = (*http.Client)(nil)
