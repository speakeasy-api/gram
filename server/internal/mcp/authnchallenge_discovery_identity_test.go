package mcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestConsentDiscoveryRequiresResolvedLiveHumanChallenge(t *testing.T) {
	t.Parallel()
	service := &Service{identityValidator: mcpidentity.NewValidatorBoundary()}
	now := time.Now()
	user := urn.NewUserSubject("user_test")
	state := AuthnChallengeState{ID: uuid.NewString(), Subject: &user, AuthorizerUserID: user.ID, AuthorizerImpersonated: new(false), CreatedAt: now}
	ctx := service.stampConsentDiscovery(t.Context(), state)
	identity, ok := mcpidentity.FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindConsentDiscovery, identity.Kind())
	require.Equal(t, user.ID, identity.UserID())
	require.Equal(t, now.Add(state.TTL()), identity.ExpiresAt())
	states := []AuthnChallengeState{
		{Subject: &user, AuthorizerUserID: user.ID, AuthorizerImpersonated: new(true), CreatedAt: now},
		{Subject: &user, AuthorizerUserID: user.ID, CreatedAt: now},
		{Subject: &user, CreatedAt: now}, // Synthetic probe or legacy unproven subject.
		{Subject: &user, AuthorizerUserID: "another-user", AuthorizerImpersonated: new(false), CreatedAt: now},
		{Subject: &user, AuthorizerUserID: user.ID, AuthorizerImpersonated: new(false), CreatedAt: now.Add(-11 * time.Minute)},
		{Subject: &user, AuthorizerUserID: user.ID, AuthorizerImpersonated: new(false), CreatedAt: now.Add(time.Hour)},
		{Subject: &user, AuthorizerUserID: user.ID, AuthorizerImpersonated: new(false), Federation: &FederatedChallenge{}, CreatedAt: now},
		{Subject: new(urn.NewAgentSubject(uuid.New())), AuthorizerUserID: user.ID, AuthorizerImpersonated: new(false), CreatedAt: now},
		{Subject: new(urn.NewAnonymousSubject(uuid.NewString())), AuthorizerUserID: user.ID, AuthorizerImpersonated: new(false), CreatedAt: now},
		{Subject: new(urn.NewAPIKeySubject(uuid.New())), AuthorizerUserID: user.ID, AuthorizerImpersonated: new(false), CreatedAt: now},
		{AuthorizerUserID: user.ID, AuthorizerImpersonated: new(false), CreatedAt: now},
	}
	for i, state := range states {
		_, ok := mcpidentity.FromContext(service.stampConsentDiscovery(ctx, state))
		require.False(t, ok, "state %d must not retain inherited provenance", i)
	}
}
