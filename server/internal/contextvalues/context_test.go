package contextvalues

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSupportSessionRequiresValidatedContext(t *testing.T) {
	t.Parallel()
	base := &AuthContext{ActiveOrganizationID: "org_123", IsAdmin: true, SupportOrganizationID: "org_123"}
	require.False(t, IsSupportSession(SetAuthContext(t.Context(), base)))
	require.True(t, IsSupportSession(WithValidatedSupportSession(t.Context(), base)))

	mismatch := *base
	mismatch.SupportOrganizationID = "org_other"
	require.False(t, IsSupportSession(WithValidatedSupportSession(t.Context(), &mismatch)))

	nonAdmin := *base
	nonAdmin.IsAdmin = false
	require.False(t, IsSupportSession(WithValidatedSupportSession(t.Context(), &nonAdmin)))

}

func TestValidatedGramSessionProvenanceIsPrivateAndPropagatesLegacyImpersonation(t *testing.T) {
	t.Parallel()

	base := &AuthContext{UserID: "user_123"}
	untrusted := SetAuthContext(t.Context(), base)
	require.False(t, HasValidatedGramSession(untrusted))
	require.False(t, IsLegacyImpersonatedSession(untrusted))

	validated := WithValidatedGramSession(t.Context(), base, false)
	require.True(t, HasValidatedGramSession(validated))
	require.False(t, IsLegacyImpersonatedSession(validated))

	impersonated := WithValidatedGramSession(t.Context(), base, true)
	require.True(t, HasValidatedGramSession(impersonated))
	require.True(t, IsLegacyImpersonatedSession(impersonated))
}

func TestRefreshSessionCookieIgnoresNilCallback(t *testing.T) {
	t.Parallel()

	ctx := WithSessionCookieRefresh(t.Context(), nil)
	require.NotPanics(t, func() { RefreshSessionCookie(ctx, "session-id") })
}
