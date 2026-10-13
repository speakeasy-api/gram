package remotesessions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEMAProviderReadinessPreservesFallbackAndErrors(t *testing.T) {
	t.Parallel()
	issuerID := uuid.New()
	order := EMAProviderOrder{Providers: []EMAProvider{
		{Kind: EMAProviderExternal, RemoteSessionIssuerID: uuid.New()},
		{Kind: EMAProviderSpeakeasy, InternalSessionIssuerID: issuerID},
	}}
	for _, ready := range []bool{false, true} {
		got, err := ApplyEMAProviderReadiness(t.Context(), "org-test", order, func(_ context.Context, organizationID string, id uuid.UUID) (bool, error) {
			require.Equal(t, "org-test", organizationID)
			require.Equal(t, issuerID, id)
			return ready, nil
		})
		require.NoError(t, err)
		require.True(t, got.Providers[0].Available(), "external fallback stays eligible")
		require.Equal(t, ready, got.Providers[1].Available())
		require.True(t, order.Providers[1].Available(), "decorating a view must not mutate the saved order")
	}
	lookupError := errors.New("temporary authority lookup failure")
	_, err := ApplyEMAProviderReadiness(t.Context(), "org-test", order, func(context.Context, string, uuid.UUID) (bool, error) {
		return false, lookupError
	})
	require.ErrorIs(t, err, lookupError, "unknown readiness must not become a definitive refusal")
}
