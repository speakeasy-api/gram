package remotesessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRefreshIssuerMetadataRequiresConfiguredTunnelTransport(t *testing.T) {
	t.Parallel()

	issuer := repo.RemoteSessionIssuer{
		Issuer:              "https://idp.example.com",
		TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))

	_, _, err := refreshIssuerMetadata(t.Context(), policy, nil, issuer)
	require.ErrorContains(t, err, "select issuer discovery transport: tunnel transport is not configured")
}
