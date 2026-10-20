package remotesessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
)

func TestClassifyClientCredentialError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "client not found", err: ErrClientCredentialClientNotFound, want: ErrClientCredentialMisconfigured},
		{name: "registration cannot authenticate", err: fmt.Errorf("%w: public client", ErrTokenEndpointConfiguration), want: ErrClientCredentialMisconfigured},
		{name: "invalid client", err: &TokenEndpointError{StatusCode: http.StatusUnauthorized, Code: oautherr.CodeInvalidClient}, want: ErrClientCredentialMisconfigured},
		{name: "invalid scope", err: &TokenEndpointError{StatusCode: http.StatusBadRequest, Code: oautherr.CodeInvalidScope}, want: ErrClientCredentialMisconfigured},
		{name: "request timeout", err: &TokenEndpointError{StatusCode: http.StatusRequestTimeout}, want: ErrClientCredentialUnavailable},
		{name: "rate limited", err: &TokenEndpointError{StatusCode: http.StatusTooManyRequests}, want: ErrClientCredentialUnavailable},
		{name: "server error", err: &TokenEndpointError{StatusCode: http.StatusServiceUnavailable}, want: ErrClientCredentialUnavailable},
		{name: "unreachable", err: &TokenEndpointError{Transport: true}, want: ErrClientCredentialUnavailable},
		{name: "signing unavailable", err: &TokenEndpointError{Signing: true}, want: ErrClientCredentialUnavailable},
		{name: "cache outage", err: errors.New("redis unavailable"), want: ErrClientCredentialUnavailable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classifyClientCredentialError(tc.err)
			require.ErrorIs(t, got, tc.want)
			require.ErrorIs(t, got, tc.err)
			require.ErrorIs(t, got, ErrNoValidToken)
		})
	}
}

func TestClientCredential_FormatsRedacted(t *testing.T) {
	t.Parallel()

	cred := NewClientCredential("secret-token", ClientCredentialSchemeBearer, time.Now().Add(time.Hour), nil)

	require.Equal(t, "secret-token", cred.Value())
	require.Equal(t, "[redacted client credential]", cred.String())
	require.Equal(t, "[redacted client credential]", fmt.Sprintf("%#v", cred))
	require.Equal(t, "[redacted client credential]", cred.LogValue().String())

	encoded, err := json.Marshal(cred)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(encoded))
}

func TestClientCredential_ForgetWithoutSourceIsFinal(t *testing.T) {
	t.Parallel()

	forgotten, err := NewClientCredential("api-key", ClientCredentialSchemeBearer, time.Now().Add(time.Hour), nil).Forget(t.Context())
	require.NoError(t, err)
	require.False(t, forgotten)
}

func TestNeedsRegistrationRotation_SkipsSelfClient(t *testing.T) {
	t.Parallel()

	rejectedAt := time.Now()
	client := Client{IssuerRegistrationEndpoint: "https://issuer.example.com/register", UpstreamRejectedAt: &rejectedAt, CredentialOwner: CredentialOwnerSelf}

	_, needed := client.needsRegistrationRotation(time.Now())
	require.False(t, needed)

	client.CredentialOwner = CredentialOwnerSubject
	trigger, needed := client.needsRegistrationRotation(time.Now())
	require.True(t, needed)
	require.Equal(t, RotationTriggerUpstreamRejected, trigger)
}

func TestClientCredentialExpiryIsTheServingCutoff(t *testing.T) {
	t.Parallel()

	cutoff := time.Now().Add(4 * time.Minute)
	expiry := clientCredentialExpiry(NewClientCredential("token", ClientCredentialSchemeBearer, cutoff, nil))
	require.NotNil(t, expiry)
	require.True(t, cutoff.Equal(*expiry))
	require.Nil(t, clientCredentialExpiry(NewClientCredential("token", ClientCredentialSchemeBearer, time.Time{}, nil)), "a source that sets no cutoff reports none")
}
