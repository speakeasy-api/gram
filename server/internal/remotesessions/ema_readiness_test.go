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
	external := EMAProvider{Kind: EMAProviderExternal, RemoteSessionIssuerID: uuid.New()}
	order := EMAProviderOrder{Providers: []EMAProvider{external, {Kind: EMAProviderSpeakeasy, InternalSessionIssuerID: issuerID}}}
	for _, reason := range []EMAProviderUnavailableReason{"", EMAProviderSigningPaused, EMAProviderSignInEnforcement} {
		got, err := ApplyEMAProviderReadiness(t.Context(), "org-test", order, func(_ context.Context, organizationID string, id uuid.UUID) (EMAProviderUnavailableReason, error) {
			require.Equal(t, "org-test", organizationID)
			require.Equal(t, issuerID, id, "external providers have no Speakeasy readiness")
			return reason, nil
		})
		require.NoError(t, err)
		require.True(t, got.Providers[0].Available())
		require.Equal(t, reason, got.Providers[1].Unavailable)
		require.True(t, order.Providers[1].Available(), "decorating a view must not mutate the stored order")
	}
	lookupError := errors.New("temporary authority lookup failure")
	got, err := ApplyEMAProviderReadiness(t.Context(), "org-test", order, func(context.Context, string, uuid.UUID) (EMAProviderUnavailableReason, error) {
		return "", lookupError
	})
	require.ErrorIs(t, err, lookupError)
	require.Equal(t, EMAProviderReadinessUnknown, got.Providers[1].Unavailable, "unknown readiness is reported, never a definitive refusal")
	require.Equal(t, []EMAProvider{external}, got.Available(), "the external provider still serves")
}

func TestEMAProviderOrderServes(t *testing.T) {
	t.Parallel()
	speakeasy := EMAProvider{Kind: EMAProviderSpeakeasy, InternalSessionIssuerID: uuid.New()}
	external := EMAProvider{Kind: EMAProviderExternal, RemoteSessionIssuerID: uuid.New()}
	untrusted := EMAProvider{Kind: EMAProviderExternal, RemoteSessionIssuerID: uuid.New(), Unavailable: EMAProviderNotTrustedByEndpoint}
	order := EMAProviderOrder{Providers: []EMAProvider{untrusted, external, speakeasy}}

	require.Equal(t, []EMAProvider{external, speakeasy}, order.Available())
	require.True(t, order.Serves(speakeasy), "a provider after the first serves while it is in the order")
	require.True(t, order.Serves(external))
	untrusted.Unavailable = ""
	require.False(t, order.Serves(untrusted), "a provider that cannot serve never serves")
	require.False(t, order.Serves(EMAProvider{Kind: EMAProviderSpeakeasy, InternalSessionIssuerID: uuid.New()}), "a replaced issuer is not in the order")
}
