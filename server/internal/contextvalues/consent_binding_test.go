package contextvalues

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestConsentBindingProvenanceIsScopedAndNotASession(t *testing.T) {
	t.Parallel()
	project, agent, issuer := uuid.New(), uuid.New(), uuid.New()
	ctx := WithConsentBindingAuthorization(t.Context(), "human", "org", project, agent, issuer)
	scope, ok := GetConsentBindingAuthorization(ctx)
	require.True(t, ok)
	require.Equal(t, agent, scope.AgentID)
	require.Equal(t, issuer, scope.IssuerID)
	require.False(t, HasValidatedGramSession(ctx))
	auth, _ := GetAuthContext(ctx)
	require.Nil(t, auth.SessionID)
	copied := *auth
	copied.UserID = "other"
	_, ok = GetConsentBindingAuthorization(SetAuthContext(ctx, &copied))
	require.False(t, ok)
	copied = *auth
	copied.ActiveOrganizationID = "other"
	_, ok = GetConsentBindingAuthorization(SetAuthContext(ctx, &copied))
	require.False(t, ok)
	copied = *auth
	otherProject := uuid.New()
	copied.ProjectID = &otherProject
	_, ok = GetConsentBindingAuthorization(SetAuthContext(ctx, &copied))
	require.False(t, ok)
	for name, alternate := range map[string]context.Context{
		"attribution only":     SetAuthContext(t.Context(), &AuthContext{UserID: "human", ActiveOrganizationID: "org", ProjectID: &project}),
		"session":              WithValidatedGramSession(ctx, auth, false),
		"oauth":                SetOAuthClientID(ctx, "oauth-client"),
		"assistant":            SetAssistantPrincipal(ctx, AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()}),
		"scope override":       SetRBACScopeOverride(ctx, "root"),
		"acting surface":       SetActingSurface(ctx, ActingSurfacePlatformMCP),
		"legacy impersonation": WithValidatedGramSession(ctx, auth, true),
		"agent actor":          WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.String())),
		"other human actor":    WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, "other")),
		"missing actor":        WithAuthenticatedActor(ctx, auth, urn.Principal{}),
		"invalid human actor":  WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, "")),
		"principal credential": WithPrincipalCredentialAuthorization(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.String()), PrincipalCredential{}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := GetConsentBindingAuthorization(alternate)
			require.False(t, ok)
		})
	}
}

func TestConsentBindingRejectsConflictingAuthProvenance(t *testing.T) {
	t.Parallel()
	ctx := WithConsentBindingAuthorization(t.Context(), "human", "org", uuid.New(), uuid.New(), uuid.New())
	auth, _ := GetAuthContext(ctx)
	for name, modify := range map[string]func(*AuthContext){
		"session ID":       func(a *AuthContext) { session := "session"; a.SessionID = &session },
		"empty session ID": func(a *AuthContext) { session := ""; a.SessionID = &session },
		"API key ID":       func(a *AuthContext) { a.APIKeyID = "key" },
		"API key name":     func(a *AuthContext) { a.APIKeyName = "key" },
		"API key scopes":   func(a *AuthContext) { a.APIKeyScopes = []string{"root"} },
		"plugin hooks key": func(a *AuthContext) { a.OrgWidePluginHooksKey = true },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			copied := *auth
			modify(&copied)
			require.Same(t, auth.consentBinding, copied.consentBinding)
			_, ok := GetConsentBindingAuthorization(SetAuthContext(ctx, &copied))
			require.False(t, ok)
		})
	}
	t.Run("support session", func(t *testing.T) {
		t.Parallel()
		copied := *auth
		copied.IsAdmin = true
		copied.SupportOrganizationID = copied.ActiveOrganizationID
		support := WithValidatedSupportSession(ctx, &copied)
		require.True(t, IsSupportSession(support))
		_, ok := GetConsentBindingAuthorization(support)
		require.False(t, ok)
	})
}

func TestConsentBindingRejectsAPIKeyAuthorizationWithoutPublicFields(t *testing.T) {
	t.Parallel()
	ctx := WithConsentBindingAuthorization(t.Context(), "human", "org", uuid.New(), uuid.New(), uuid.New())
	auth, _ := GetAuthContext(ctx)
	principalModeOnly := *auth
	principalModeOnly.apiKeyAuthorizationMode = APIKeyAuthorizationModePrincipal
	for name, alternate := range map[string]context.Context{
		"legacy helper":        WithLegacyAPIKeyAuthorization(ctx, auth),
		"principal helper":     WithPrincipalAPIKeyAuthorization(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID), PrincipalCredential{}),
		"principal mode alone": SetAuthContext(ctx, &principalModeOnly),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			copied, ok := GetAuthContext(alternate)
			require.True(t, ok)
			require.Same(t, auth.consentBinding, copied.consentBinding)
			require.Empty(t, copied.APIKeyID)
			require.Empty(t, copied.APIKeyName)
			require.Empty(t, copied.APIKeyScopes)
			require.False(t, copied.OrgWidePluginHooksKey)
			_, ok = APIKeyAuthorization(alternate)
			require.True(t, ok)
			_, ok = GetConsentBindingAuthorization(alternate)
			require.False(t, ok)
		})
	}
}
