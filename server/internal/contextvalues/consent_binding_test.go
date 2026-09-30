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
		"principal credential": WithPrincipalCredentialAuthorization(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.String()), PrincipalCredential{}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := GetConsentBindingAuthorization(alternate)
			require.False(t, ok)
		})
	}
}
