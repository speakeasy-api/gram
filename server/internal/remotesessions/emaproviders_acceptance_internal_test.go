package remotesessions

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestStampEMAProviderLastAccepted_AttributesEachProvider(t *testing.T) {
	t.Parallel()
	oktaA, oktaB, speakeasyOld, speakeasyNew := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	early := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)
	accepted := EMAVendorAcceptance{
		{kind: EMAProviderExternal, issuerID: oktaA}:         early,
		{kind: EMAProviderSpeakeasy, issuerID: speakeasyOld}: late,
	}
	order := EMAProviderOrder{VendorIssuer: "https://vendor.example.com", IsDefault: false, Providers: []EMAProvider{
		{Kind: EMAProviderExternal, RemoteSessionIssuerID: oktaA, InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil},
		{Kind: EMAProviderExternal, RemoteSessionIssuerID: oktaB, InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil},
		{Kind: EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: speakeasyNew, Unavailable: "", LastAcceptedAt: nil},
	}}

	stamped := StampEMAProviderLastAccepted(order, accepted)
	require.NotNil(t, stamped.Providers[0].LastAcceptedAt)
	require.Equal(t, early, *stamped.Providers[0].LastAcceptedAt)
	require.Nil(t, stamped.Providers[1].LastAcceptedAt, "another external issuer's acceptance is not this one's")
	require.Nil(t, stamped.Providers[2].LastAcceptedAt, "a replaced Speakeasy issuer's acceptance does not carry over")
	require.Nil(t, order.Providers[0].LastAcceptedAt, "the input order is not mutated")

	placeholder := EMAProviderOrder{VendorIssuer: order.VendorIssuer, IsDefault: true, Providers: []EMAProvider{
		{Kind: EMAProviderExternal, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil},
	}}
	accepted[emaAcceptanceKey{kind: EMAProviderExternal, issuerID: oktaB}] = late
	stamped = StampEMAProviderLastAccepted(placeholder, accepted)
	require.NotNil(t, stamped.Providers[0].LastAcceptedAt)
	require.Equal(t, late, *stamped.Providers[0].LastAcceptedAt, "the default's external entry stands for any endpoint's issuer, so it takes the newest")
}
