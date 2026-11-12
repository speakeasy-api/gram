package identitychaining

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

// Runtime always sends people back to the consent page; only the consent sign-in is judged ineligible.
func TestSpeakeasyAuthorizeIdentityJudgesEligibilityOnTheConsentSignIn(t *testing.T) {
	t.Parallel()
	source := speakeasySource{}
	runtime := Request{UserID: "user-1"}
	consent := runtime
	consent.consentSignInID = uuid.New()
	for _, tc := range []struct {
		req    Request
		reason signins.Reason
		want   Reason
	}{
		{runtime, signins.ReasonNotMember, ReasonReauthenticationRequired},
		{runtime, signins.ReasonMethodNotAllowed, ReasonReauthenticationRequired},
		{consent, signins.ReasonNotMember, ReasonIneligibleIdentity},
		{consent, signins.ReasonMethodNotAllowed, ReasonIneligibleIdentity},
		{consent, signins.ReasonSessionExpired, ReasonReauthenticationRequired},
	} {
		outcome := source.authorizeIdentity(t.Context(), testenv.NewLogger(t), tc.req, &selection{}, signins.Result{Reason: tc.reason, Identity: nil})
		require.Equal(t, tc.want, outcome.Reason, "%s consent=%v", tc.reason, tc.req.consent())
		require.False(t, outcome.Retryable)
	}
	other := source.authorizeIdentity(t.Context(), testenv.NewLogger(t), consent, &selection{}, signins.Result{Reason: signins.ReasonEligible, Identity: &signins.Identity{UserID: "user-2"}})
	require.Equal(t, ReasonReauthenticationRequired, other.Reason, "another person's sign-in needs this person's own")
}
