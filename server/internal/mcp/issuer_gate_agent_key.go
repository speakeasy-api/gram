package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// authenticateIssuerGateAgentKey deliberately does not use authenticateToken:
// legacy keys and user credentials must not bypass the issuer's session gate.
func (s *Service) authenticateIssuerGateAgentKey(ctx context.Context, token string, endpoint *ResolvedMcpEndpoint) (context.Context, *urn.SessionSubject, error) {
	// Authorize authenticates the key and performs live principal admission
	// against its immutable delegated policy. Do not synthesize session policy.
	keyCtx, err := s.auth.AuthorizeWithPostAuthenticationCheck(ctx, token, &security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         nil,
		RequiredScopes: []string{"consumer"},
	}, func(ctx context.Context) error {
		mode, keyOK := contextvalues.APIKeyAuthorization(ctx)
		actor, actorOK := contextvalues.AuthenticatedActor(ctx)
		if !keyOK || mode != contextvalues.APIKeyAuthorizationModePrincipal || !actorOK || actor.Type != urn.PrincipalTypeAgent {
			return oops.C(oops.CodeUnauthorized)
		}
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil || authCtx.ActiveOrganizationID != endpoint.OrganizationID || (authCtx.ProjectID != nil && *authCtx.ProjectID != endpoint.ProjectID) {
			return oops.C(oops.CodeUnauthorized)
		}
		if enabled, _ := s.agentAuthorizationRollout(ctx, s.logger, endpoint); !enabled {
			return oops.C(oops.CodeNotFound)
		}
		return nil
	})
	if err != nil {
		return ctx, nil, fmt.Errorf("authenticate issuer-gated agent key: %w", err)
	}
	mode, keyOK := contextvalues.APIKeyAuthorization(keyCtx)
	actor, actorOK := contextvalues.AuthenticatedActor(keyCtx)
	authCtx, authOK := contextvalues.GetAuthContext(keyCtx)
	if !keyOK || mode != contextvalues.APIKeyAuthorizationModePrincipal || !actorOK || actor.Type != urn.PrincipalTypeAgent ||
		!authOK || authCtx == nil || authCtx.APIKeyID == "" || authCtx.ActiveOrganizationID != endpoint.OrganizationID ||
		(authCtx.ProjectID != nil && *authCtx.ProjectID != endpoint.ProjectID) {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	keyCtx, err = s.requireAgentSessionAuthorization(keyCtx, endpoint)
	if err != nil {
		return ctx, nil, err
	}
	agentID, err := uuid.Parse(actor.ID)
	if err != nil || agentID == uuid.Nil {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	// Bind organization-wide agent keys only after authorizing this endpoint.
	projectAuth := *authCtx
	projectAuth.ProjectID = &endpoint.ProjectID
	keyCtx = contextvalues.SetAuthContext(keyCtx, &projectAuth)
	subject := urn.NewAgentSubject(agentID)
	return s.identityValidator.StampAgent(keyCtx, agentID), &subject, nil
}

// stampAuthenticatedAPIKey retains the principal proven by successful key
// authorization; legacy keys remain organization-only provenance.
func (s *Service) stampAuthenticatedAPIKey(ctx context.Context) (context.Context, error) {
	mode, ok := contextvalues.APIKeyAuthorization(ctx)
	if !ok || mode != contextvalues.APIKeyAuthorizationModePrincipal {
		return s.identityValidator.StampAPIKey(ctx), nil
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok || actor.Type != urn.PrincipalTypeAgent {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	id, err := uuid.Parse(actor.ID)
	if err != nil || id == uuid.Nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	return s.identityValidator.StampAgent(ctx, id), nil
}
