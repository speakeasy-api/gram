package identitychaining

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/signins"
)

// Only a refusal a new sign-in replaces asks for one; the rest name the administrator.
func TestSignInRefusalReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reason signins.Reason
		want   Reason
	}{
		{signins.ReasonSessionNotFound, ReasonReauthenticationRequired},
		{signins.ReasonSessionExpired, ReasonReauthenticationRequired},
		{signins.ReasonNotLinked, ReasonReauthenticationRequired},
		{signins.ReasonNotUserSubject, ReasonReauthenticationRequired},
		{signins.ReasonSignInRevoked, ReasonReauthenticationRequired},
		{signins.ReasonExpired, ReasonReauthenticationRequired},
		{signins.ReasonEmailChanged, ReasonReauthenticationRequired},
		{signins.ReasonAuthTimeUnknown, ReasonReauthenticationRequired},
		{signins.ReasonUnmanagedAccount, ReasonIneligibleIdentity},
		{signins.ReasonMethodNotEligible, ReasonIneligibleIdentity},
		{signins.ReasonMethodNotAllowed, ReasonIneligibleIdentity},
		{signins.ReasonTenantNotAllowed, ReasonIneligibleIdentity},
		{signins.ReasonOrganizationDisabled, ReasonIneligibleIdentity},
		{signins.ReasonNotMember, ReasonIneligibleIdentity},
		{signins.ReasonDeprovisioned, ReasonIneligibleIdentity},
		{signins.ReasonEmailNotVerified, ReasonIneligibleIdentity},
		{signins.ReasonEmailDomain, ReasonIneligibleIdentity},
	} {
		require.Equal(t, tc.want, signInRefusalReason(tc.reason), tc.reason)
	}
}

func TestSpeakeasyAuthorizeIdentityKeepsTheRefusalReason(t *testing.T) {
	t.Parallel()
	source := speakeasySource{}
	req := Request{UserID: "user-1"}
	for _, tc := range []struct {
		reason signins.Reason
		want   Reason
	}{
		{signins.ReasonNotMember, ReasonIneligibleIdentity},
		{signins.ReasonSessionExpired, ReasonReauthenticationRequired},
	} {
		outcome := source.authorizeIdentity(t.Context(), nil, req, &selection{}, signins.Result{Reason: tc.reason, Identity: nil})
		require.Equal(t, tc.want, outcome.Reason, tc.reason)
		require.False(t, outcome.Retryable)
	}
	other := source.authorizeIdentity(t.Context(), nil, req, &selection{}, signins.Result{Reason: signins.ReasonEligible, Identity: &signins.Identity{UserID: "user-2"}})
	require.Equal(t, ReasonReauthenticationRequired, other.Reason, "another person's sign-in needs this person's own")
}
