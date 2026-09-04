package audit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestApplyAuthenticatedActorUsesAgentInsteadOfAuthorizer(t *testing.T) {
	t.Parallel()
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, "018f8d7b-58d7-7cc4-bb16-9f8c6b99a001")
	ctx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), &contextvalues.AuthContext{}, agent, contextvalues.PrincipalCredential{
		AuthorizerUserID: "user_authorizer",
	})
	params := repo.InsertAuditLogParams{
		ActorID:          "user_authorizer",
		ActorType:        string(urn.PrincipalTypeUser),
		ActorDisplayName: conv.ToPGText("Authorizer Name"),
		ActorSlug:        conv.ToPGText("authorizer-slug"),
	}

	applyAuthenticatedActor(ctx, &params)
	require.Equal(t, agent.ID, params.ActorID)
	require.Equal(t, string(urn.PrincipalTypeAgent), params.ActorType)
	require.False(t, params.ActorDisplayName.Valid)
	require.False(t, params.ActorSlug.Valid)
}
