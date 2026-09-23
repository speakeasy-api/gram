package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/delegation"
)

// Keep the existing consumer API stable; the delegation package owns all
// retained secrets, storage operations and refresh state transitions.
type DelegationBinding = delegation.Binding
type DelegationAssertion = delegation.Assertion
type DelegationAuthorizer = delegation.Authorizer
type DelegationOfflineStatus = delegation.OfflineStatus

var (
	ErrDelegationTemporary        = delegation.ErrTemporary
	ErrDelegationReauthentication = delegation.ErrReauthentication
	ErrDelegationConfiguration    = delegation.ErrConfiguration
)

type DelegationService struct{ service *delegation.Service }

func NewDelegationService(db *pgxpool.Pool, enc *encryption.Client, manager *ChallengeManager) *DelegationService {
	return &DelegationService{service: delegation.New(db, enc, delegationDependencies(manager.LoadFederatedDelegationProvider, manager.LoadFederatedProvider, manager.RefreshFederatedIdentity, nil))}
}

func delegationDependencies(binding, provider func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error), refresh func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error), now func() time.Time) delegation.Dependencies {
	adaptLoader := func(load func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error)) func(context.Context, string, uuid.UUID, uuid.UUID) (delegation.Provider, error) {
		return func(ctx context.Context, org string, issuer, client uuid.UUID) (delegation.Provider, error) {
			p, err := load(ctx, org, issuer, client)
			if err != nil {
				return nil, delegationConfigurationError(err)
			}
			if p == nil {
				return nil, nil
			}
			return delegationProvider{p}, nil
		}
	}
	return delegation.Dependencies{Now: now, LoadBinding: adaptLoader(binding), LoadProvider: adaptLoader(provider), RefreshIdentity: func(ctx context.Context, provider delegation.Provider, token, subject, nonce string) (*delegation.RefreshResult, error) {
		p, ok := provider.(delegationProvider)
		if !ok {
			return nil, &delegation.RefreshError{Kind: delegation.RefreshConfiguration}
		}
		result, err := refresh(ctx, p.p, token, subject, nonce)
		if result == nil {
			return nil, delegationRefreshError(err)
		}
		var identity delegation.Identity
		if result.Identity != nil {
			identity = delegationIdentity{result.Identity}
		}
		return &delegation.RefreshResult{Identity: identity, Credentials: result.Credentials}, delegationRefreshError(err)
	}}
}
func delegationConfigurationError(err error) error {
	if errors.Is(err, ErrFederatedConfiguration) {
		return ErrDelegationConfiguration
	}
	return err
}
func delegationRefreshError(err error) error {
	if err == nil {
		return nil
	}
	var failure *FederatedRefreshError
	if !errors.As(err, &failure) {
		return &delegation.RefreshError{Kind: delegation.RefreshAmbiguous}
	}
	kind := delegation.RefreshAmbiguous
	switch failure.Kind {
	case FederatedRefreshAmbiguous:
		kind = delegation.RefreshAmbiguous
	case FederatedRefreshInvalidGrant:
		kind = delegation.RefreshInvalidGrant
	case FederatedRefreshConfiguration:
		kind = delegation.RefreshConfiguration
	case FederatedRefreshInvalidIdentity:
		kind = delegation.RefreshInvalidIdentity
	case FederatedRefreshRetryable:
		kind = delegation.RefreshRetryable
	}
	return &delegation.RefreshError{Kind: kind}
}

type delegationProvider struct{ p *FederatedProvider }

func (p delegationProvider) Binding(human string) DelegationBinding {
	return DelegationBinding{OrganizationID: p.p.organizationID, IssuerID: p.p.issuer.ID, ClientID: p.p.client.ID, HumanID: human}
}
func (p delegationProvider) IssuerURL() string { return p.p.issuer.Issuer }
func (p delegationProvider) DelegationConfigurationHash() string {
	return p.p.DelegationConfigurationHash()
}
func (p delegationProvider) OfflineConfigurationHash() string { return p.p.OfflineConfigurationHash() }
func (p delegationProvider) OfflineRequested() bool {
	return slices.Contains(p.p.client.Scope, "offline_access")
}
func (p delegationProvider) OfflineSupported() bool {
	policy, err := p.p.OfflinePolicy()
	return err == nil && policy.Enabled
}

type delegationIdentity struct{ i *FederatedIdentity }

func (i delegationIdentity) IssuerURL() string     { return i.i.Issuer }
func (i delegationIdentity) SubjectID() string     { return i.i.Subject }
func (i delegationIdentity) NonceValue() string    { return i.i.Nonce }
func (i delegationIdentity) Expiration() time.Time { return i.i.ExpiresAt }
func (i delegationIdentity) DiscardCredentials()   { i.i.DiscardCredentials() }
func (i delegationIdentity) WithCredentials(consume func(delegation.Credentials) error) error {
	return i.i.WithCredentials(func(c EphemeralFederatedCredentials) error { return consume(c) })
}
func (s *DelegationService) RetainVerifiedLogin(ctx context.Context, p *FederatedProvider, human string, i *FederatedIdentity, requested bool) error {
	if p == nil || i == nil {
		return ErrDelegationConfiguration
	}
	return s.service.RetainVerifiedLogin(ctx, delegationProvider{p}, human, delegationIdentity{i}, requested) //nolint:wrapcheck // Compatibility facade preserves the public sentinel error and message contract.
}
func (s *DelegationService) OfflineStatus(ctx context.Context, p *FederatedProvider, human string) (DelegationOfflineStatus, error) {
	if p == nil {
		return DelegationOfflineStatus{}, ErrDelegationConfiguration
	}
	return s.service.OfflineStatus(ctx, delegationProvider{p}, human) //nolint:wrapcheck // Compatibility facade preserves the public sentinel error and message contract.
}
func (s *DelegationService) RecordOfflineRefusal(ctx context.Context, p *FederatedProvider, human string) error {
	if p == nil {
		return ErrDelegationConfiguration
	}
	return s.service.RecordOfflineRefusal(ctx, delegationProvider{p}, human) //nolint:wrapcheck // Compatibility facade preserves the public sentinel error and message contract.
}

func (p delegationProvider) String() string               { return "[delegation provider]" }
func (p delegationProvider) GoString() string             { return p.String() }
func (p delegationProvider) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (p delegationProvider) LogValue() slog.Value         { return slog.StringValue(p.String()) }
func (i delegationIdentity) String() string               { return "[delegation identity]" }
func (i delegationIdentity) GoString() string             { return i.String() }
func (i delegationIdentity) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (i delegationIdentity) LogValue() slog.Value         { return slog.StringValue(i.String()) }

// Translate federation-specific authorization failures at the same boundary as
// provider-loading failures; the core need not import the OIDC implementation.
type delegationAuthority struct{ authority DelegationAuthorizer }

func (a delegationAuthority) AuthorizeDelegation(ctx context.Context, b DelegationBinding) error {
	return delegationConfigurationError(a.authority.AuthorizeDelegation(ctx, b))
}
func (s *DelegationService) Resolve(ctx context.Context, b DelegationBinding, authority DelegationAuthorizer) (DelegationAssertion, error) {
	if authority != nil {
		authority = delegationAuthority{authority}
	}
	return s.service.Resolve(ctx, b, authority) //nolint:wrapcheck // Compatibility facade preserves the public sentinel error and message contract.
}
func (s *DelegationService) Revoke(ctx context.Context, b DelegationBinding, authority DelegationAuthorizer) error {
	if authority != nil {
		authority = delegationAuthority{authority}
	}
	return s.service.Revoke(ctx, b, authority) //nolint:wrapcheck // Compatibility facade preserves the public sentinel error and message contract.
}
