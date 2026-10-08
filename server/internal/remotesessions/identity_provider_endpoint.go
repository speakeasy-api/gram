package remotesessions

import (
	"context"
	"fmt"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// IdentityProviderEndpoint is an organization's trusted identity provider:
// its token endpoint, bound to Speakeasy's trusted client registration, and the key
// set that signs what it issues.
type IdentityProviderEndpoint struct {
	*TokenEndpoint

	keys       *jwks.KeyResolver
	jwksURI    string
	fetchScope string
}

// LoadIdentityProviderEndpoint resolves an organization's trusted identity
// provider registration with live discovery and host validation. keys verifies
// what the provider signs. Configuration failures are ErrFederatedConfiguration
// or ErrFederatedSigning; other errors are transient.
func (m *ChallengeManager) LoadIdentityProviderEndpoint(ctx context.Context, keys *jwks.KeyResolver, organizationID string, issuerID, clientID uuid.UUID) (*IdentityProviderEndpoint, error) {
	p, err := m.LoadFederatedProvider(ctx, organizationID, issuerID, clientID)
	if err != nil {
		return nil, err
	}
	if err := m.validateFederatedMetadataHosts(ctx, p.issuer, p.metadata); err != nil {
		return nil, err
	}
	doer, auth, err := m.federatedTokenClient(p)
	if err != nil {
		return nil, err
	}
	return &IdentityProviderEndpoint{
		TokenEndpoint: &TokenEndpoint{endpoint: p.metadata.TokenEndpoint, issuer: p.issuer.Issuer, issuerID: p.issuer.ID, doer: doer, auth: auth},
		keys:          keys,
		jwksURI:       p.metadata.JwksURI,
		fetchScope:    p.issuer.ID.String(),
	}, nil
}

// VerifyAssertion checks one signature on raw against the provider's
// published keys, read over its own egress, and decodes the claims into dest.
// It validates no claims; the header comes back for checks the caller owns.
// ErrJWTKeySetUnavailable marks a key-set failure that says nothing about raw.
func (e *IdentityProviderEndpoint) VerifyAssertion(ctx context.Context, raw string, dest ...any) (jose.Header, error) {
	header, err := verifyIssuerSignedJWTWithKeyPolicy(ctx, e.keys, e.jwksURI, e.fetchScope, e.doer, raw, jwks.AllowedSignatureAlgorithms(), validateJWTVerificationKeyStrength, dest...)
	if err != nil {
		return header, fmt.Errorf("verify identity provider assertion: %w", err)
	}
	return header, nil
}
