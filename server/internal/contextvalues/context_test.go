package contextvalues

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
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

	admittedAuth, ok := GetAuthContext(admitted)
	require.True(t, ok)
	replaced := WithPrincipalCredentialAuthorization(admitted, admittedAuth, agent, PrincipalCredential{
		AuthorizerUserID:       "user_new_authorizer",
		DelegatedGrants:        []byte(`{"requested":[],"effective":[]}`),
		DelegatedGrantsVersion: 1,
	})
	_, _, ok = PrincipalCredentialProvenance(replaced)
	require.False(t, ok, "a new credential must not retain the previously admitted owner")
}

func TestValidatedGramSessionSetsCanonicalUserActor(t *testing.T) {
	t.Parallel()

	ctx := WithValidatedGramSession(t.Context(), &AuthContext{UserID: "user_123"}, false)
	actor, ok := AuthenticatedActor(ctx)
	require.True(t, ok)
	require.Equal(t, "user:user_123", actor.String())
}

func TestIsOrdinaryGramUserSessionRejectsAlternateProvenance(t *testing.T) {
	t.Parallel()
	sessionID := "session_123"
	base := &AuthContext{ActiveOrganizationID: "org_123", UserID: "user_123", SessionID: &sessionID}
	validated := WithValidatedGramSession(t.Context(), base, false)
	require.True(t, IsOrdinaryGramUserSession(validated))

	cases := map[string]func(context.Context) context.Context{
		"assistant":      func(ctx context.Context) context.Context { return SetAssistantPrincipal(ctx, AssistantPrincipal{}) },
		"OAuth":          func(ctx context.Context) context.Context { return SetOAuthClientID(ctx, "client_123") },
		"acting surface": func(ctx context.Context) context.Context { return SetActingSurface(ctx, ActingSurfacePlatformMCP) },
		"RBAC override":  func(ctx context.Context) context.Context { return SetRBACScopeOverride(ctx, "scope") },
	}
	for name, decorate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.False(t, IsOrdinaryGramUserSession(decorate(validated)))
		})
	}
}

func TestValidatedGramSessionActingUser(t *testing.T) {
	t.Parallel()
	sessionID := "session"
	base := &AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID}

	_, ok := ValidatedGramSessionActingUser(SetAuthContext(t.Context(), base))
	require.False(t, ok)

	provenance, ok := ValidatedGramSessionActingUser(WithValidatedGramSession(t.Context(), base, false))
	require.True(t, ok)
	require.Equal(t, "org", provenance.OrganizationID())
	require.Equal(t, "user", provenance.UserID())
	require.Equal(t, "session", provenance.SessionID())

	_, ok = ValidatedGramSessionActingUser(WithValidatedGramSession(t.Context(), base, true))
	require.False(t, ok, "legacy impersonation must not establish an acting user")

	support := *base
	support.IsAdmin = true
	support.SupportOrganizationID = "org"
	supportCtx := WithValidatedGramSession(t.Context(), &support, false)
	supportCtx = WithValidatedSupportSession(supportCtx, &support)
	_, ok = ValidatedGramSessionActingUser(supportCtx)
	require.False(t, ok, "support sessions must not establish an acting user")

	demo := *base
	demo.ActiveOrganizationID = constants.DemoOrganizationID
	_, ok = ValidatedGramSessionActingUser(WithValidatedGramSession(t.Context(), &demo, false))
	require.False(t, ok, "shared demo sessions have no active membership and remain unsupported")

	missingSession := *base
	missingSession.SessionID = nil
	_, ok = ValidatedGramSessionActingUser(WithValidatedGramSession(t.Context(), &missingSession, false))
	require.False(t, ok)

	for name, authCtx := range map[string]*AuthContext{
		"api key attribution":        {ActiveOrganizationID: "org", UserID: "creator", APIKeyID: "key"},
		"assistant/chat attribution": {ActiveOrganizationID: "org", UserID: "owner", ExternalUserID: "assistant"},
		"anonymous organization":     {ActiveOrganizationID: "org"},
	} {
		_, ok = ValidatedGramSessionActingUser(SetAuthContext(t.Context(), authCtx))
		require.False(t, ok, name)
	}
}

func TestValidatedGramSessionActingUserUsesImmutableIdentitySnapshot(t *testing.T) {
	t.Parallel()

	sessionID := "session-original"
	source := &AuthContext{ActiveOrganizationID: "org-original", UserID: "user-original", SessionID: &sessionID}
	ctx := WithValidatedGramSession(t.Context(), source, false)

	// Mutate both the source pointer and the AuthContext pointer carried by ctx.
	// Neither can substitute a different identity after validation.
	sessionID = "session-source-mutated"
	source.ActiveOrganizationID = "org-source-mutated"
	source.UserID = "user-source-mutated"
	authCtx, ok := GetAuthContext(ctx)
	require.True(t, ok)
	mutatedSessionID := "session-context-mutated"
	authCtx.ActiveOrganizationID = "org-context-mutated"
	authCtx.UserID = "user-context-mutated"
	authCtx.SessionID = &mutatedSessionID

	provenance, ok := ValidatedGramSessionActingUser(ctx)
	require.True(t, ok)
	require.Equal(t, "org-original", provenance.OrganizationID())
	require.Equal(t, "user-original", provenance.UserID())
	require.Equal(t, "session-original", provenance.SessionID())
}

func TestValidatedGramSessionActingUserRejectsAlternateProvenance(t *testing.T) {
	t.Parallel()

	sessionID := "session"
	base := &AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID}
	validated := WithValidatedGramSession(t.Context(), base, false)
	cases := map[string]func(context.Context) context.Context{
		"assistant":      func(ctx context.Context) context.Context { return SetAssistantPrincipal(ctx, AssistantPrincipal{}) },
		"OAuth":          func(ctx context.Context) context.Context { return SetOAuthClientID(ctx, "client") },
		"acting surface": func(ctx context.Context) context.Context { return SetActingSurface(ctx, ActingSurfacePlatformMCP) },
		"RBAC override":  func(ctx context.Context) context.Context { return SetRBACScopeOverride(ctx, "scope") },
	}
	for name, decorate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := ValidatedHostedInferenceActingUser(decorate(validated))
			require.False(t, ok)
		})
	}
}

func TestValidatedChatSessionActingUserRequiresCompleteNonDemoStamp(t *testing.T) {
	t.Parallel()

	ctx := WithValidatedChatSessionActingUser(t.Context(), "org", "user", "session")
	provenance, ok := ValidatedChatSessionActingUser(ctx)
	require.True(t, ok)
	require.Equal(t, "org", provenance.OrganizationID())
	require.Equal(t, "user", provenance.UserID())
	require.Equal(t, "session", provenance.SessionID())

	for name, values := range map[string][3]string{
		"missing organization": {"", "user", "session"},
		"missing user":         {"org", "", "session"},
		"missing session":      {"org", "user", ""},
		"shared demo":          {constants.DemoOrganizationID, "user", "session"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			unstamped := WithValidatedChatSessionActingUser(t.Context(), values[0], values[1], values[2])
			_, ok := ValidatedChatSessionActingUser(unstamped)
			require.False(t, ok)
		})
	}
}

func TestRefreshSessionCookieIgnoresNilCallback(t *testing.T) {
	t.Parallel()

	ctx := WithSessionCookieRefresh(t.Context(), nil)
	require.NotPanics(t, func() { RefreshSessionCookie(ctx, "session-id") })
}
