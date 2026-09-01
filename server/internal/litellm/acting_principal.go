package litellm

import (
	"context"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/litellm/repo"
	"github.com/speakeasy-api/gram/server/internal/litellmacting"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// MintActingPrincipal converts only an ordinary authenticated Gram user session
// into a short-lived assertion for one active managed LiteLLM invocation.
func (s *Service) MintActingPrincipal(ctx context.Context, payload *gen.MintActingPrincipalPayload) (*gen.LitellmActingPrincipalResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || !contextvalues.IsOrdinaryGramUserSession(ctx) {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if payload == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "acting-principal payload is required")
	}
	instanceID, err := uuid.Parse(payload.InstanceID)
	if err != nil || instanceID == uuid.Nil || instanceID.String() != payload.InstanceID {
		return nil, oops.E(oops.CodeBadRequest, err, "instance_id must be a canonical UUID")
	}
	invocationID, err := uuid.Parse(payload.InvocationID)
	if err != nil || invocationID == uuid.Nil || invocationID.Version() != 7 || invocationID.String() != payload.InvocationID {
		return nil, oops.E(oops.CodeBadRequest, err, "invocation_id must be a canonical UUIDv7")
	}

	mintContext, err := repo.New(s.db).GetLiteLLMActingPrincipalMintContext(ctx, repo.GetLiteLLMActingPrincipalMintContextParams{
		UserID: authCtx.UserID, OrganizationID: authCtx.ActiveOrganizationID, ID: instanceID, ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve LiteLLM acting-principal mint context").LogError(ctx, s.logger)
	}
	if !mintContext.ActiveMember {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if mintContext.ID == uuid.Nil || mintContext.ApiKeyID == uuid.Nil {
		return nil, oops.E(oops.CodeNotFound, nil, "active managed LiteLLM instance not found")
	}
	if s.actingSigner == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "LiteLLM acting-principal signer is unavailable").LogError(ctx, s.logger)
	}
	assertion, err := s.actingSigner.MintAssertion(authCtx.UserID, litellmacting.AssertionBinding{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: authCtx.ProjectID.String(),
		InstanceID: mintContext.ID.String(), APIKeyID: mintContext.ApiKeyID.String(), InvocationID: payload.InvocationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "mint LiteLLM acting-principal assertion")
	}
	return &gen.LitellmActingPrincipalResult{
		Assertion: assertion, ContractVersion: litellmacting.ContractVersion, InvocationID: payload.InvocationID, ExpiresIn: int(litellmacting.AssertionLifetime.Seconds()),
	}, nil
}
