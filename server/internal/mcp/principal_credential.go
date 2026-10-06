package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// authenticatePrincipalCredential authenticates and admits a principal
// credential as its agent or workload principal, the way an agent API key is
// admitted when it authenticates. The mcp:connect check runs where it runs
// for every other principal credential.
func (s *Service) authenticatePrincipalCredential(ctx context.Context, token string) (context.Context, error) {
	authed, err := s.principalCredentials.Authenticate(ctx, token)
	if errors.Is(err, principalcredential.ErrInvalid) || errors.Is(err, principalcredential.ErrNotCredential) {
		return ctx, oops.E(oops.CodeUnauthorized, err, "invalid principal credential").LogWarn(ctx, s.logger)
	}
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "authenticate principal credential").LogError(ctx, s.logger)
	}
	authed, err = s.authz.PrepareContext(authed)
	if err != nil {
		return ctx, fmt.Errorf("admit principal credential: %w", err)
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
// endpoint's mcp:connect check. Unlike an agent key, it does not re-evaluate
// the agent authorization rollout: only the server mints principal
// credentials.
func (s *Service) authenticateIssuerGatePrincipalCredential(ctx context.Context, token string, endpoint *ResolvedMcpEndpoint) (context.Context, *urn.SessionSubject, error) {
	authed, err := s.authenticatePrincipalCredential(ctx, token)
	if err != nil {
		return ctx, nil, fmt.Errorf("%w: %w", errCredentialRejected, err)
	}
	if err := requirePrincipalCredentialProject(authed, endpoint.ProjectID); err != nil {
		return ctx, nil, fmt.Errorf("%w: %w", errCredentialRejected, err)
	}
	authed, err = s.requireAgentSessionAuthorization(authed, endpoint)
	if err != nil {
		return ctx, nil, fmt.Errorf("%w: %w", errCredentialRejected, err)
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

// requirePrincipalCredentialProject rejects a request authenticated with a
// principal credential for a resource outside the credential's project.
func requirePrincipalCredentialProject(ctx context.Context, project uuid.UUID) error {
	credential, ok := principalcredential.FromContext(ctx)
	if ok && credential.Credential.ProjectID != project {
		return oops.E(oops.CodeForbidden, nil, "principal credential project does not match the resource project")
	}
	return nil
}
