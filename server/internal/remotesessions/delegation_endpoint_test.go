package remotesessions_test

import (
	"context"
	"errors"
	"testing"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"
)

// A rejected dashboard session must not fall back to legacy producer-key auth.
func TestDelegationStatusEndpointRequiresSession(t *testing.T) {
	t.Parallel()
	rejected := errors.New("session required")
	calls := 0
	endpoint := orgclientsgen.NewGetClientDelegationStatusEndpoint(nil, func(ctx context.Context, _ string, scheme *security.APIKeyScheme) (context.Context, error) {
		calls++
		require.Equal(t, constants.SessionSecurityScheme, scheme.Name)
		return ctx, rejected
	})
	_, err := endpoint(t.Context(), &orgclientsgen.GetClientDelegationStatusPayload{ID: "00000000-0000-0000-0000-000000000001"})
	require.ErrorIs(t, err, rejected)
	require.Equal(t, 1, calls)
}
