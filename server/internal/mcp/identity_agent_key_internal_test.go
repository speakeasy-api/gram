package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestStampAuthenticatedAPIKeyAgentAttribution(t *testing.T) {
	t.Parallel()
	s := &Service{identityValidator: mcpidentity.NewValidatorBoundary()}
	id := uuid.New()
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, id.String())
	credential := contextvalues.PrincipalCredential{AuthorizerUserID: "user_authorizer", DelegatedGrants: []byte(`[]`), DelegatedGrantsVersion: 1}
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{}, actor, credential)
	ctx = contextvalues.WithPrincipalCredentialOwner(ctx, "user_owner")
	stamped, err := s.stampAuthenticatedAPIKey(ctx)
	require.NoError(t, err)
	identity, ok := mcpidentity.FromContext(stamped)
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindAgent, identity.Kind())
	require.Equal(t, id.String(), identity.AgentID())
	require.Empty(t, identity.UserID())
	gotActor, ok := contextvalues.AuthenticatedActor(stamped)
	require.True(t, ok)
	require.Equal(t, actor, gotActor)
	gotCredential, ok := contextvalues.PrincipalCredentialAuthorization(stamped)
	require.True(t, ok)
	require.Equal(t, credential, gotCredential)
	authorizer, owner, ok := contextvalues.PrincipalCredentialProvenance(stamped)
	require.True(t, ok)
	require.Equal(t, "user_authorizer", authorizer)
	require.Equal(t, "user_owner", owner)
}

func TestStampAuthenticatedAPIKeyRejectsInvalidPrincipal(t *testing.T) {
	t.Parallel()
	s := &Service{identityValidator: mcpidentity.NewValidatorBoundary()}
	for _, actor := range []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeAgent, "invalid"),
		urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.Nil.String()),
		urn.NewPrincipal(urn.PrincipalTypeUser, "user_owner"),
	} {
		ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{}, actor, contextvalues.PrincipalCredential{})
		_, err := s.stampAuthenticatedAPIKey(ctx)
		require.Error(t, err)
	}
}

func TestStampAuthenticatedAPIKeyLegacyRemainsUnattributed(t *testing.T) {
	t.Parallel()
	s := &Service{identityValidator: mcpidentity.NewValidatorBoundary()}
	ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{})
	stamped, err := s.stampAuthenticatedAPIKey(ctx)
	require.NoError(t, err)
	identity, ok := mcpidentity.FromContext(stamped)
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindAPIKey, identity.Kind())
	require.Empty(t, identity.AgentID())
	require.Empty(t, identity.UserID())
}
