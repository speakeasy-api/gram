package remotesessions

import (
	"context"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
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
	return preparationAPIView(result), nil
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
	view := preparationAPIView(result)
	if view.EmaProviders, err = s.endpointEMAProviders(ctx, in, result.Issuer); err != nil {
		return nil, err
	}
	return view, nil
}

// endpointEMAProviders is the order identity chaining resolves for the
// endpoint, or nil when the vendor issuer is not an issuer identifier.
func (s *Service) endpointEMAProviders(ctx context.Context, in PreparationInput, issuer string) (*types.EMAProviderOrder, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	vendor, err := NormalizeVendorIssuer(issuer)
	if err != nil || authCtx == nil {
		return nil, nil //nolint:nilnil // No order for an issuer that is not an identifier.
	}
	q := repo.New(s.db)
	order, err := EndpointEMAProviders(ctx, q, authCtx.ActiveOrganizationID, in.UserSessionIssuerID, vendor, s.speakeasySigningAvailable)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, s.logger)
	}
	target := EMACredentialTarget{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource}
	if order, err = ApplyEMAProviderLastAccepted(ctx, q, target, order); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ema provider last accepted").LogError(ctx, s.logger)
	}
	return s.liveEMAProvidersView(ctx, s.logger, authCtx.ActiveOrganizationID, order), nil
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
		EmaProviders: nil,
	}
	if r.BindingID != uuid.Nil {
		out.BindingID = conv.PtrEmpty(r.BindingID.String())
	}
	if r.ClientID != uuid.Nil {
		out.ClientID = conv.PtrEmpty(r.ClientID.String())
	}
	return out
}
