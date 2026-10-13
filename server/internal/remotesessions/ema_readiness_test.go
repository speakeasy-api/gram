package remotesessions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEMAProviderReadinessKeepsReasonAndMarksUnknown(t *testing.T) {
	t.Parallel()
	issuerID := uuid.New()
	selection := EMAProviderSelection{Provider: EMAProvider{Kind: EMAProviderSpeakeasy, InternalSessionIssuerID: issuerID}}
	for _, reason := range []EMAProviderUnavailableReason{"", EMAProviderSigningPaused, EMAProviderSignInEnforcement} {
		got, err := ApplyEMAProviderReadiness(t.Context(), "org-test", selection, func(_ context.Context, organizationID string, id uuid.UUID) (EMAProviderUnavailableReason, error) {
			require.Equal(t, "org-test", organizationID)
			require.Equal(t, issuerID, id)
			return reason, nil
		})
		require.NoError(t, err)
		require.Equal(t, reason, got.Provider.Unavailable)
		require.True(t, selection.Provider.Available(), "decorating a view must not mutate the stored selection")
	}
	lookupError := errors.New("temporary authority lookup failure")
	got, err := ApplyEMAProviderReadiness(t.Context(), "org-test", selection, func(context.Context, string, uuid.UUID) (EMAProviderUnavailableReason, error) {
		return "", lookupError
	})
	require.ErrorIs(t, err, lookupError)
	require.Equal(t, EMAProviderReadinessUnknown, got.Provider.Unavailable, "unknown readiness is reported, never a definitive refusal")

	external := EMAProviderSelection{Provider: EMAProvider{Kind: EMAProviderExternal, RemoteSessionIssuerID: uuid.New()}}
	got, err = ApplyEMAProviderReadiness(t.Context(), "org-test", external, func(context.Context, string, uuid.UUID) (EMAProviderUnavailableReason, error) {
		t.Fatal("external providers have no Speakeasy readiness")
		return "", nil
	})
	require.NoError(t, err)
	require.True(t, got.Provider.Available())
}
