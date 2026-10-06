package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// authenticatePrincipalCredential authenticates a principal credential as its
// agent or workload principal. Admission and the mcp:connect check run where
// they run for every other principal credential.
func (s *Service) authenticatePrincipalCredential(ctx context.Context, token string) (context.Context, error) {
	authed, err := s.principalCredentials.Authenticate(ctx, token)
	if errors.Is(err, principalcredential.ErrInvalid) || errors.Is(err, principalcredential.ErrNotCredential) {
		return ctx, oops.E(oops.CodeUnauthorized, err, "invalid principal credential").LogWarn(ctx, s.logger)
	}
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "authenticate principal credential").LogError(ctx, s.logger)
	}
	credential, _ := principalcredential.FromContext(authed)
	if credential.Credential.Principal.Type == urn.PrincipalTypeAgent {
		agentID, err := uuid.Parse(credential.Credential.Principal.ID)
		if err != nil {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		return s.identityValidator.StampAgent(authed, agentID), nil
	}
	return authed, nil
}

// authenticateIssuerGatePrincipalCredential admits a principal credential at
// an issuer-gated endpoint the way an agent API key is admitted: in the
// endpoint's tenant, through credential or workload admission and the
// endpoint's mcp:connect check. Only the server mints principal credentials,
// and it does so only where the agent identity rollout allows, so the
// endpoint does not re-evaluate the rollout.
func (s *Service) authenticateIssuerGatePrincipalCredential(ctx context.Context, token string, endpoint *ResolvedMcpEndpoint) (context.Context, *urn.SessionSubject, error) {
	authed, err := s.authenticatePrincipalCredential(ctx, token)
	if err != nil {
		return ctx, nil, fmt.Errorf("%w: %w", errCredentialRejected, err)
	}
	authCtx, ok := contextvalues.GetAuthContext(authed)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID != endpoint.OrganizationID || authCtx.ProjectID == nil || *authCtx.ProjectID != endpoint.ProjectID {
		return ctx, nil, fmt.Errorf("%w: %w", errCredentialRejected, oops.C(oops.CodeUnauthorized))
	}
	authed, err = s.authz.PrepareContext(authed)
	if err != nil {
		return ctx, nil, fmt.Errorf("prepare principal credential authorization: %w", err)
	}
	authed, err = s.requireAgentSessionAuthorization(authed, endpoint)
	if err != nil {
		return ctx, nil, err
	}
	credential, _ := principalcredential.FromContext(authed)
	principal := credential.Credential.Principal
	var subject urn.SessionSubject
	if principal.Type == urn.PrincipalTypeAgent {
		subject = urn.NewAgentSubject(uuid.MustParse(principal.ID))
	} else {
		issuerID, externalSubject, err := principal.Workload()
		if err != nil {
			return ctx, nil, oops.C(oops.CodeUnauthorized)
		}
		subject = urn.NewWorkloadSubject(issuerID, externalSubject)
	}
	return authed, &subject, nil
}
