package litellm

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/litellm/repo"
	"github.com/speakeasy-api/gram/server/internal/litellmacting"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestMintActingPrincipalUsesOnlyValidatedSessionAndCurrentManagedKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Empty(t, authCtx.APIKeyID)
	created, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "principal-mint", FailurePosture: "fail_closed"})
	require.NoError(t, err)
	instanceID, err := uuid.Parse(created.Instance.ID)
	require.NoError(t, err)
	mintContext, err := repo.New(ti.conn).GetLiteLLMActingPrincipalMintContext(ctx, repo.GetLiteLLMActingPrincipalMintContextParams{
		ID: instanceID, UserID: authCtx.UserID, OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	signer, err := litellmacting.NewSigner("principal-mint-test-secret")
	require.NoError(t, err)
	ti.service.actingSigner = signer
	invocationID, err := uuid.NewV7()
	require.NoError(t, err)
	payload := &gen.MintActingPrincipalPayload{InstanceID: created.Instance.ID, InvocationID: invocationID.String()}

	unvalidatedAuth := &contextvalues.AuthContext{ActiveOrganizationID: authCtx.ActiveOrganizationID, UserID: authCtx.UserID, ProjectID: authCtx.ProjectID}
	unvalidatedCtx := contextvalues.SetAuthContext(t.Context(), unvalidatedAuth)
	_, err = ti.service.MintActingPrincipal(unvalidatedCtx, payload)
	requireOops(t, err, oops.CodeUnauthorized)

	validatedCtx := contextvalues.WithValidatedGramSession(ctx, authCtx, false)
	result, err := ti.service.MintActingPrincipal(validatedCtx, payload)
	require.NoError(t, err)
	require.Equal(t, litellmacting.ContractVersion, result.ContractVersion)
	require.Equal(t, invocationID.String(), result.InvocationID)
	require.Equal(t, 60, result.ExpiresIn)
	identity, err := signer.VerifyAssertion(result.Assertion, litellmacting.AssertionBinding{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: authCtx.ProjectID.String(), InstanceID: mintContext.ID.String(), APIKeyID: mintContext.ApiKeyID.String(), InvocationID: invocationID.String(),
	})
	require.NoError(t, err)
	require.Equal(t, authCtx.UserID, identity.UserID)
	require.NotEmpty(t, identity.JTI)

	impersonatedCtx := contextvalues.WithValidatedGramSession(ctx, authCtx, true)
	_, err = ti.service.MintActingPrincipal(impersonatedCtx, payload)
	requireOops(t, err, oops.CodeUnauthorized)

	badInvocation := *payload
	badInvocation.InvocationID = uuid.NewString()
	_, err = ti.service.MintActingPrincipal(validatedCtx, &badInvocation)
	requireOops(t, err, oops.CodeBadRequest)

	wrongProjectAuth := *authCtx
	wrongProjectID := uuid.New()
	wrongProjectAuth.ProjectID = &wrongProjectID
	wrongProjectCtx := contextvalues.WithValidatedGramSession(contextvalues.SetAuthContext(ctx, &wrongProjectAuth), &wrongProjectAuth, false)
	_, err = ti.service.MintActingPrincipal(wrongProjectCtx, payload)
	requireOops(t, err, oops.CodeNotFound)

	require.NoError(t, orgrepo.New(ti.conn).DeleteOrganizationUserRelationship(validatedCtx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID, UserID: conv.ToPGText(authCtx.UserID),
	}))
	_, err = ti.service.MintActingPrincipal(validatedCtx, payload)
	requireOops(t, err, oops.CodeUnauthorized)
}

func TestMintActingPrincipalRejectsRevokedInstance(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "revoked-principal-mint", FailurePosture: "fail_closed"})
	require.NoError(t, err)
	require.NoError(t, ti.service.RevokeInstance(ctx, &gen.RevokeInstancePayload{ID: created.Instance.ID}))
	signer, err := litellmacting.NewSigner("principal-mint-revoked-secret")
	require.NoError(t, err)
	ti.service.actingSigner = signer
	invocationID, err := uuid.NewV7()
	require.NoError(t, err)
	validatedCtx := contextvalues.WithValidatedGramSession(ctx, authCtx, false)
	_, err = ti.service.MintActingPrincipal(validatedCtx, &gen.MintActingPrincipalPayload{InstanceID: created.Instance.ID, InvocationID: invocationID.String()})
	requireOops(t, err, oops.CodeNotFound)
}
