package remotesessions

import (
	"context"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// preparationAPIInput authenticates the management surface before parsing object
// identifiers. Core preparation additionally validates every selected object's
// tenant and issuer relationship inside its transaction.
func (s *Service) preparationAPIInput(ctx context.Context, userID, issuerID, resource string, write bool) (PreparationInput, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return PreparationInput{}, oops.C(oops.CodeUnauthorized)
	}
	scope := authz.ScopeProjectRead
	if write {
		scope = authz.ScopeProjectWrite
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return PreparationInput{}, err
	}
	user, err := uuid.Parse(userID)
	if err != nil {
		return PreparationInput{}, oops.E(oops.CodeBadRequest, err, "invalid user session issuer id")
	}
	issuer, err := uuid.Parse(issuerID)
	if err != nil {
		return PreparationInput{}, oops.E(oops.CodeBadRequest, err, "invalid remote session issuer id")
	}
	return PreparationInput{
		UserSessionIssuerID: user, RemoteSessionIssuerID: issuer, Resource: resource,
		ClientID: uuid.Nil, Scopes: nil, Mechanism: "", ConfirmGrants: nil,
		ExpectedGeneration: 0, TokenEndpointAuthMethod: "", ResourceMetadata: nil,
	}, nil
}

func (s *Service) PrepareEMA(ctx context.Context, p *gen.PrepareEMAPayload) (*gen.IdentityChainingPreparation, error) {
	in, err := s.preparationAPIInput(ctx, p.UserSessionIssuerID, p.RemoteSessionIssuerID, p.Resource, true)
	if err != nil {
		return nil, err
	}
	if p.ClientID != nil {
		in.ClientID, err = uuid.Parse(*p.ClientID)
		if err != nil || in.ClientID == uuid.Nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid remote session client id")
		}
	}
	in.Scopes, in.ConfirmGrants, in.Mechanism, in.ExpectedGeneration = p.Scopes, p.ConfirmGrants, p.Mechanism, p.ExpectedGeneration
	in.TokenEndpointAuthMethod = conv.PtrValOr(p.TokenEndpointAuthMethod, "")
	if p.ResourceMetadata != nil {
		in.ResourceMetadata = &PreparationResourceMetadata{Resource: p.ResourceMetadata.Resource, AuthorizationServers: p.ResourceMetadata.AuthorizationServers}
	}
	result, err := s.PrepareIdentityChaining(ctx, in)
	if err != nil {
		return nil, err
	}
	// The binding is already committed; report it without connection_scopes
	// (the error is logged) rather than failing a write that succeeded.
	_ = s.reportConnectionScopes(ctx, in.UserSessionIssuerID, result)
	return preparationAPIView(result), nil
}

// reportConnectionScopes adds the server connection's current scopes to a
// management response, so the dashboard can say when Turn on would change
// what identity chaining requests.
func (s *Service) reportConnectionScopes(ctx context.Context, userSessionIssuerID uuid.UUID, r *PreparationResult) error {
	project, org, err := s.preparationTenant(ctx, false)
	if err != nil {
		return err
	}
	if err := reportConnectionScopes(ctx, s.db, project, org, userSessionIssuerID, s.resources != nil, r); err != nil {
		return oops.E(oops.CodeUnexpected, err, "read identity chaining connection scopes").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) ReadEMA(ctx context.Context, p *gen.ReadEMAPayload) (*gen.IdentityChainingPreparation, error) {
	in, err := s.preparationAPIInput(ctx, p.UserSessionIssuerID, p.RemoteSessionIssuerID, p.Resource, false)
	if err != nil {
		return nil, err
	}
	result, err := s.ReadIdentityChaining(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := s.reportConnectionScopes(ctx, in.UserSessionIssuerID, result); err != nil {
		return nil, err
	}
	view := preparationAPIView(result)
	if view.EmaProviders, view.LastAcceptedAt, err = s.endpointEMAProviders(ctx, in, result.Issuer); err != nil {
		return nil, err
	}
	return view, nil
}

// endpointEMAProviders is the order identity chaining resolves for the
// endpoint, with each provider's organization-wide acceptance by the vendor
// for callers with org:read, and when the vendor last accepted an assertion
// for this binding from a provider the order tries and that can serve now.
// Both are nil when the vendor issuer is not an issuer identifier.
func (s *Service) endpointEMAProviders(ctx context.Context, in PreparationInput, issuer string) (*types.EMAProviderOrder, *string, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	vendor, err := NormalizeVendorIssuer(issuer)
	if err != nil || authCtx == nil {
		return nil, nil, nil
	}
	org := authCtx.ActiveOrganizationID
	logger := s.logger.With(attr.SlogOrganizationID(org))
	q := repo.New(s.db)
	order, err := EndpointEMAProviders(ctx, q, org, in.UserSessionIssuerID, vendor, s.speakeasySigningAvailable)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
	}
	order, err = ApplyEMAProviderReadiness(ctx, org, order, s.speakeasyReadiness)
	if err != nil {
		logger.WarnContext(ctx, "check Speakeasy issuer readiness", attr.SlogError(err))
	}
	// Acceptance is status evidence: a failed read leaves it out rather than
	// failing the whole read. Organization-wide evidence needs org:read.
	orgReader, err := s.authz.Evaluate(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: org, Dimensions: nil})
	if err != nil {
		logger.WarnContext(ctx, "check organization read access", attr.SlogError(err))
	}
	if orgReader {
		if accepted, err := VendorEMAAcceptance(ctx, q, org, vendor); err != nil {
			logger.WarnContext(ctx, "read ema vendor acceptance", attr.SlogError(err))
		} else {
			order = StampEMAProviderLastAccepted(order, accepted)
		}
	}
	var bindingAccepted *string
	target := EMACredentialTarget{OrganizationID: org, ProjectID: *authCtx.ProjectID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource}
	if accepted, err := EMABindingAcceptance(ctx, q, target); err != nil {
		logger.WarnContext(ctx, "read ema binding acceptance", attr.SlogError(err))
	} else if at := ServingLastAccepted(order, accepted); at != nil {
		bindingAccepted = conv.PtrEmpty(at.UTC().Format(time.RFC3339))
	}
	return EMAProviderOrderView(order), bindingAccepted, nil
}

func (s *Service) UnlinkEMA(ctx context.Context, p *gen.UnlinkEMAPayload) (*gen.IdentityChainingPreparation, error) {
	in, err := s.preparationAPIInput(ctx, p.UserSessionIssuerID, p.RemoteSessionIssuerID, p.Resource, true)
	if err != nil {
		return nil, err
	}
	in.ExpectedGeneration = p.ExpectedGeneration
	result, err := s.UnlinkIdentityChaining(ctx, in)
	if err != nil {
		return nil, err
	}
	return preparationAPIView(result), nil
}

func preparationAPIView(r *PreparationResult) *gen.IdentityChainingPreparation {
	out := &gen.IdentityChainingPreparation{
		BindingID: nil, ClientID: nil,
		State: r.State, Stage: r.Stage, Remediation: r.Remediation, Retryable: r.Retryable,
		Generation: r.Generation, ExternalClientID: conv.PtrEmpty(r.ExternalClientID), Issuer: conv.PtrEmpty(r.Issuer),
		Resource: r.Resource, GrantTypes: r.GrantTypes, Scopes: append([]string{}, r.Scopes...), GrantSource: r.GrantSource,
		EmaProviders: nil, LastAcceptedAt: nil, ConnectionScopes: r.ConnectionScopes, ScopesOutOfDate: nil,
	}
	if r.ConnectionScopes != nil {
		out.ScopesOutOfDate = new(r.ScopesOutOfDate)
	}
	if r.BindingID != uuid.Nil {
		out.BindingID = conv.PtrEmpty(r.BindingID.String())
	}
	if r.ClientID != uuid.Nil {
		out.ClientID = conv.PtrEmpty(r.ClientID.String())
	}
	return out
}
