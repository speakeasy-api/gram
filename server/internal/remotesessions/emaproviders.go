package remotesessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// EMAProviderKind names a source of ID-JAGs for identity chaining.
type EMAProviderKind string

const (
	// EMAProviderExternal is an external issuer, such as an Okta
	// authorization server, that exchanges the human's ID token for an ID-JAG.
	EMAProviderExternal EMAProviderKind = "external"

	// EMAProviderSpeakeasy is the organization's internal session issuer.
	EMAProviderSpeakeasy EMAProviderKind = "speakeasy"
)

// SpeakeasyEMASigning reports whether this build implements internal ID-JAG signing.
// Runtime sources additionally require a configured signer and an enabled issuer.
const SpeakeasyEMASigning = true

// EMAProviderUnavailableReason says why a provider in the order cannot serve.
type EMAProviderUnavailableReason string

const (
	EMAProviderDeleted              EMAProviderUnavailableReason = "deleted"
	EMAProviderDisabled             EMAProviderUnavailableReason = "disabled"
	EMAProviderNotTrustedByEndpoint EMAProviderUnavailableReason = "not_trusted_by_endpoint"
	EMAProviderSigningUnavailable   EMAProviderUnavailableReason = "signing_unavailable"
)

// EMAProvider is one entry in a vendor's EMA provider order. An external
// entry with no remote session issuer stands for the endpoint's trusted
// issuer in the default order.
type EMAProvider struct {
	Kind                    EMAProviderKind
	RemoteSessionIssuerID   uuid.UUID
	InternalSessionIssuerID uuid.UUID
	Unavailable             EMAProviderUnavailableReason
}

// Available reports whether the provider can serve now.
func (p EMAProvider) Available() bool { return p.Unavailable == "" }

// EMAProviderOrder is a vendor's EMA provider order, saved or default.
type EMAProviderOrder struct {
	VendorIssuer string
	IsDefault    bool
	Providers    []EMAProvider
}

// Available lists the providers that can serve now, in order.
func (o EMAProviderOrder) Available() []EMAProvider {
	out := make([]EMAProvider, 0, len(o.Providers))
	for _, p := range o.Providers {
		if p.Available() {
			out = append(out, p)
		}
	}
	return out
}

// EMAProviderScope is what a provider order is resolved for.
type EMAProviderScope struct {
	// Endpoint, when true, resolves for one endpoint: external providers are
	// eligible only when they are TrustedIssuer, the issuer whose subject
	// token identity chaining holds for the human.
	Endpoint      bool
	TrustedIssuer uuid.NullUUID

	// SpeakeasySigns reports whether the internal session issuer can sign.
	SpeakeasySigns bool
}

// ErrInvalidVendorIssuer marks a vendor issuer that is not an issuer identifier.
var ErrInvalidVendorIssuer = errors.New("remotesessions: vendor issuer must be an absolute http(s) issuer identifier")

// NormalizeVendorIssuer keys a vendor's EMA provider order by its issuer, in
// the same canonical form issuer lookups use.
func NormalizeVendorIssuer(issuer string) (string, error) {
	canonical, err := issuerurl.Parse(issuer)
	if err != nil {
		return "", ErrInvalidVendorIssuer
	}
	return canonical.String(), nil
}

// ResolveEMAProviders reads the explicitly selected provider for a vendor.
// An unset selection preserves the endpoint's external issuer; enabling the
// Speakeasy issuer alone never changes an existing integration's authority.
func ResolveEMAProviders(ctx context.Context, q *repo.Queries, organizationID, vendorIssuer string, scope EMAProviderScope) (EMAProviderOrder, error) {
	order := EMAProviderOrder{VendorIssuer: vendorIssuer, IsDefault: false, Providers: []EMAProvider{}}
	rows, err := q.ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: organizationID, VendorIssuer: vendorIssuer})
	if err != nil {
		return order, fmt.Errorf("list ema providers: %w", err)
	}
	// Retain the first selection from older multi-provider rows, without falling
	// through if it becomes unavailable. New writes accept at most one provider.
	if len(rows) > 1 {
		rows = rows[:1]
	}
	for _, row := range rows {
		p := EMAProvider{Kind: EMAProviderSpeakeasy, RemoteSessionIssuerID: row.RemoteSessionIssuerID.UUID, InternalSessionIssuerID: row.InternalSessionIssuerID.UUID, Unavailable: ""}
		if row.RemoteSessionIssuerID.Valid {
			p.Kind = EMAProviderExternal
		}
		switch {
		case row.ProviderDeleted:
			p.Unavailable = EMAProviderDeleted
		case row.ProviderDisabled:
			p.Unavailable = EMAProviderDisabled
		case p.Kind == EMAProviderExternal && scope.Endpoint && (!scope.TrustedIssuer.Valid || scope.TrustedIssuer.UUID != p.RemoteSessionIssuerID):
			p.Unavailable = EMAProviderNotTrustedByEndpoint
		case p.Kind == EMAProviderSpeakeasy && !scope.SpeakeasySigns:
			p.Unavailable = EMAProviderSigningUnavailable
		}
		order.Providers = append(order.Providers, p)
	}
	if len(rows) > 0 {
		return order, nil
	}

	order.IsDefault = true
	order.Providers = append(order.Providers, EMAProvider{Kind: EMAProviderExternal, RemoteSessionIssuerID: scope.TrustedIssuer.UUID, InternalSessionIssuerID: uuid.Nil, Unavailable: ""})
	return order, nil
}

// EndpointEMAProviders resolves vendorIssuer's order for one endpoint, whose
// external provider is the user session issuer's trusted issuer. The
// management API and identity chaining both read the order through it.
func EndpointEMAProviders(ctx context.Context, q *repo.Queries, organizationID string, userSessionIssuerID uuid.UUID, vendorIssuer string, speakeasySigns bool) (EMAProviderOrder, error) {
	trusted := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	issuer, err := q.GetEMAChainingUserIssuer(ctx, repo.GetEMAChainingUserIssuerParams{ID: userSessionIssuerID, OrganizationID: conv.ToPGText(organizationID)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return EMAProviderOrder{VendorIssuer: vendorIssuer, IsDefault: false, Providers: nil}, fmt.Errorf("read identity chaining user session issuer: %w", err)
	default:
		trusted = issuer.TrustedRemoteSessionIssuerID
	}
	return ResolveEMAProviders(ctx, q, organizationID, vendorIssuer, EMAProviderScope{Endpoint: true, TrustedIssuer: trusted, SpeakeasySigns: speakeasySigns})
}

// EMAProviderReadiness checks live signing eligibility for an organization issuer.
// A false result is an authoritative refusal; an error means readiness is unknown.
type EMAProviderReadiness func(context.Context, string, uuid.UUID) (bool, error)

// ApplyEMAProviderReadiness decorates a database order without minting tokens or
// requiring a user grant. Call after releasing transactions: readiness may query
// the database and external flag providers.
func ApplyEMAProviderReadiness(ctx context.Context, organizationID string, order EMAProviderOrder, check EMAProviderReadiness) (EMAProviderOrder, error) {
	order.Providers = append([]EMAProvider(nil), order.Providers...)
	for i, provider := range order.Providers {
		if provider.Kind != EMAProviderSpeakeasy || !provider.Available() {
			continue
		}
		if check == nil {
			continue
		}
		ready, err := check(ctx, organizationID, provider.InternalSessionIssuerID)
		if err != nil {
			return order, fmt.Errorf("check issuer readiness: %w", err)
		}
		if !ready {
			order.Providers[i].Unavailable = EMAProviderSigningUnavailable
		}
	}
	return order, nil
}
