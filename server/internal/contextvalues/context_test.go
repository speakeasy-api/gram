package contextvalues

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
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

func TestAuthContextHasOneCanonicalTypedActor(t *testing.T) {
	t.Parallel()

	principalType := reflect.TypeFor[urn.Principal]()
	authContextType := reflect.TypeFor[AuthContext]()
	count := 0
	for field := range authContextType.Fields() {
		if field.Type == principalType {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestAuthenticatedActorAndCredentialProvenanceAreIndependent(t *testing.T) {
	t.Parallel()

	untrusted := SetAuthContext(t.Context(), &AuthContext{UserID: "user_untrusted", APIKeyID: "key_untrusted"})
	_, ok := AuthenticatedActor(untrusted)
	require.False(t, ok)
	_, ok = APIKeyAuthorization(untrusted)
	require.False(t, ok)

	legacy := WithLegacyAPIKeyAuthorization(t.Context(), &AuthContext{
		UserID: "user_authorizer", APIKeyID: "key_123", APIKeyScopes: []string{"agent", "agent_user"},
	})
	actor, ok := AuthenticatedActor(legacy)
	require.True(t, ok)
	require.Equal(t, "user:user_authorizer", actor.String())
	mode, ok := APIKeyAuthorization(legacy)
	require.True(t, ok)
	require.Equal(t, APIKeyAuthorizationModeLegacy, mode)
	legacyAuth, ok := GetAuthContext(legacy)
	require.True(t, ok)
	require.Equal(t, "key_123", legacyAuth.APIKeyID)

	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	policy := []byte(`{"requested":[],"effective":[]}`)
	principalBacked := WithPrincipalAPIKeyAuthorization(t.Context(), &AuthContext{
		UserID: "user_authorizer", APIKeyID: "key_agent",
	}, agent, PrincipalCredential{
		AuthorizerUserID:       "user_authorizer",
		DelegatedGrants:        policy,
		DelegatedGrantsVersion: 1,
	})
	policy[0] = 'x'
	actor, ok = AuthenticatedActor(principalBacked)
	require.True(t, ok)
	require.Equal(t, agent, actor)
	mode, ok = APIKeyAuthorization(principalBacked)
	require.True(t, ok)
	require.Equal(t, APIKeyAuthorizationModePrincipal, mode)
	credential, ok := PrincipalCredentialAuthorization(principalBacked)
	require.True(t, ok)
	require.Equal(t, "user_authorizer", credential.AuthorizerUserID)
	require.JSONEq(t, `{"requested":[],"effective":[]}`, string(credential.DelegatedGrants))
	require.False(t, func() bool {
		_, _, ok := PrincipalCredentialProvenance(principalBacked)
		return ok
	}())

	admitted := WithPrincipalCredentialOwner(principalBacked, "user_owner")
	authorizer, owner, ok := PrincipalCredentialProvenance(admitted)
	require.True(t, ok)
	require.Equal(t, "user_authorizer", authorizer)
	require.Equal(t, "user_owner", owner)
	admittedActor, ok := AuthenticatedActor(admitted)
	require.True(t, ok)
	require.Equal(t, agent, admittedActor)
}

func TestValidatedGramSessionSetsCanonicalUserActor(t *testing.T) {
	t.Parallel()

	ctx := WithValidatedGramSession(t.Context(), &AuthContext{UserID: "user_123"}, false)
	actor, ok := AuthenticatedActor(ctx)
	require.True(t, ok)
	require.Equal(t, "user:user_123", actor.String())
}

func TestRefreshSessionCookieIgnoresNilCallback(t *testing.T) {
	t.Parallel()

	ctx := WithSessionCookieRefresh(t.Context(), nil)
	require.NotPanics(t, func() { RefreshSessionCookie(ctx, "session-id") })
}
