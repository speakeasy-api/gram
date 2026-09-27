package remotesessions

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/delegation"
	"github.com/stretchr/testify/require"
)

func TestDelegationAdapterPreservesOneShotCredentials(t *testing.T) {
	t.Parallel()
	p := federatedFixture(t)
	identity := delegationLogin(p, time.Now(), "secret-assertion", "secret-refresh", time.Hour)
	adapter := delegationIdentity{identity}
	calls := 0
	consume := func(c delegation.Credentials) error {
		calls++
		require.Equal(t, "secret-assertion", c.IDToken())
		require.Equal(t, "secret-refresh", c.RefreshToken())
		return nil
	}
	require.NoError(t, adapter.WithCredentials(consume))
	require.Error(t, adapter.WithCredentials(consume))
	require.Equal(t, 1, calls)
	for _, value := range []any{adapter, delegationProvider{p}} {
		require.NotContains(t, fmt.Sprintf("%v %+v %#v", value, value, value), "secret-")
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.JSONEq(t, "{}", string(encoded))
	}
}

func TestDelegationAdapterTranslatesDefinitiveAuthorizationFailure(t *testing.T) {
	t.Parallel()
	authority := delegationAuthority{authority: delegationTestAuthority(func(context.Context, DelegationBinding) error {
		return fmt.Errorf("provider registration: %w", ErrFederatedConfiguration)
	})}
	require.ErrorIs(t, authority.AuthorizeDelegation(t.Context(), DelegationBinding{}), delegation.ErrConfiguration)
	for _, tc := range []struct {
		source FederatedRefreshFailure
		want   delegation.RefreshFailure
	}{
		{FederatedRefreshAmbiguous, delegation.RefreshAmbiguous},
		{FederatedRefreshInvalidGrant, delegation.RefreshInvalidGrant},
		{FederatedRefreshConfiguration, delegation.RefreshConfiguration},
		{FederatedRefreshInvalidIdentity, delegation.RefreshInvalidIdentity},
		{FederatedRefreshRetryable, delegation.RefreshRetryable},
	} {
		var failure *delegation.RefreshError
		require.ErrorAs(t, delegationRefreshError(&FederatedRefreshError{Kind: tc.source}), &failure)
		require.Equal(t, tc.want, failure.Kind)
	}
}
