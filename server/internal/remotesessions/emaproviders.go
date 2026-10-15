package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// EMAProviderUnavailableReason says why a provider in the order cannot serve.
type EMAProviderUnavailableReason string

const (
	EMAProviderDeleted              EMAProviderUnavailableReason = "deleted"
	EMAProviderDisabled             EMAProviderUnavailableReason = "disabled"
	EMAProviderNotTrustedByEndpoint EMAProviderUnavailableReason = "not_trusted_by_endpoint"
	// EMAProviderSigningUnavailable means this deployment cannot sign.
	EMAProviderSigningUnavailable EMAProviderUnavailableReason = "signing_unavailable"
	EMAProviderNotRolledOut       EMAProviderUnavailableReason = "not_rolled_out"
	EMAProviderVerifiedDomain     EMAProviderUnavailableReason = "verified_domain_required"
	EMAProviderSetupRequired      EMAProviderUnavailableReason = "setup_required"
	EMAProviderKeyless            EMAProviderUnavailableReason = "keyless"
	EMAProviderSigningPaused      EMAProviderUnavailableReason = "signing_paused"
	// EMAProviderSignInEnforcement means the organization's sign-in
	// enforcement is off, so Speakeasy cannot vouch for a sign-in.
	EMAProviderSignInEnforcement EMAProviderUnavailableReason = "sign_in_enforcement_required"
	// EMAProviderReadinessUnknown marks a status view whose live readiness
	// read failed. Runtime never stores or acts on it.
	EMAProviderReadinessUnknown EMAProviderUnavailableReason = "readiness_unknown"
)

// EMAProvider is one entry in a vendor's EMA provider order. An external
// entry with no remote session issuer stands for the endpoint's trusted
// issuer in the default order.
type EMAProvider struct {
	Kind                    EMAProviderKind
	RemoteSessionIssuerID   uuid.UUID
	InternalSessionIssuerID uuid.UUID
	Unavailable             EMAProviderUnavailableReason
	// LastAcceptedAt is when the vendor last accepted an assertion from the
	// provider for one binding target. Status views set it; runtime never does.
	LastAcceptedAt *time.Time
}

// Available reports whether the provider can serve now.
func (p EMAProvider) Available() bool { return p.Unavailable == "" }

// Same reports whether p and o name the same provider, ignoring availability.
func (p EMAProvider) Same(o EMAProvider) bool {
	return p.Kind == o.Kind && p.RemoteSessionIssuerID == o.RemoteSessionIssuerID && p.InternalSessionIssuerID == o.InternalSessionIssuerID
}

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

// Serves reports whether p is in the order and can serve now.
func (o EMAProviderOrder) Serves(p EMAProvider) bool {
	for _, candidate := range o.Providers {
		if candidate.Available() && candidate.Same(p) {
			return true
		}
	}
	return false
}

// EMAProviderScope is what an order is resolved for.
type EMAProviderScope struct {
	// Endpoint, when true, resolves for one endpoint: an external provider is
	// eligible only when it is TrustedIssuer, the issuer whose subject token
	// identity chaining holds for the human.
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

// ResolveEMAProviders reads a vendor's saved provider order. Providers that
// cannot serve are kept and flagged, so views can say why and runtime skips
// them. No saved order derives the default: the endpoint's trusted external
// issuer first, then the organization's live internal session issuer
// (Speakeasy) when it has one. Setting up the Speakeasy issuer therefore
// adds it after every endpoint's external issuer by default; an
// organization-wide view, which stands for no one endpoint, keeps an
// external placeholder with no issuer id in front of it. With neither, the
// default is that single external placeholder.
func ResolveEMAProviders(ctx context.Context, q *repo.Queries, organizationID, vendorIssuer string, scope EMAProviderScope) (EMAProviderOrder, error) {
	order := EMAProviderOrder{VendorIssuer: vendorIssuer, IsDefault: false, Providers: []EMAProvider{}}
	rows, err := q.ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: organizationID, VendorIssuer: vendorIssuer})
	if err != nil {
		return order, fmt.Errorf("list ema providers: %w", err)
	}
	for _, row := range rows {
		p := EMAProvider{Kind: EMAProviderSpeakeasy, RemoteSessionIssuerID: row.RemoteSessionIssuerID.UUID, InternalSessionIssuerID: row.InternalSessionIssuerID.UUID, Unavailable: "", LastAcceptedAt: nil}
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
	external := EMAProvider{Kind: EMAProviderExternal, RemoteSessionIssuerID: scope.TrustedIssuer.UUID, InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil}
	if !scope.Endpoint || scope.TrustedIssuer.Valid {
		order.Providers = append(order.Providers, external)
	}
	internal, err := q.GetEMAInternalSessionIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return order, fmt.Errorf("read internal session issuer: %w", err)
	default:
		p := EMAProvider{Kind: EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: internal.ID, Unavailable: "", LastAcceptedAt: nil}
		switch {
		case internal.Disabled:
			p.Unavailable = EMAProviderDisabled
		case !scope.SpeakeasySigns:
			p.Unavailable = EMAProviderSigningUnavailable
		}
		order.Providers = append(order.Providers, p)
	}
	if len(order.Providers) == 0 {
		order.Providers = append(order.Providers, external)
	}
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

// EMACredentialTarget is one binding target whose cached credentials say
// when the vendor last accepted each provider's assertion.
type EMACredentialTarget struct {
	OrganizationID        string
	ProjectID             uuid.UUID
	UserSessionIssuerID   uuid.UUID
	RemoteSessionIssuerID uuid.UUID
	Resource              string
}

// ApplyEMAProviderLastAccepted stamps a status view's providers with when the
// vendor last accepted an assertion from each, from target's newest live
// cached credential. The Speakeasy entry takes the speakeasy credential; an
// external entry the endpoint trusts takes the external one.
func ApplyEMAProviderLastAccepted(ctx context.Context, q *repo.Queries, target EMACredentialTarget, order EMAProviderOrder) (EMAProviderOrder, error) {
	rows, err := q.ListEMACredentialLastAccepted(ctx, repo.ListEMACredentialLastAcceptedParams{
		OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, UserSessionIssuerID: target.UserSessionIssuerID,
		RemoteSessionIssuerID: target.RemoteSessionIssuerID, Resource: target.Resource,
	})
	if err != nil {
		return order, fmt.Errorf("list ema credential last accepted: %w", err)
	}
	accepted := make(map[EMAProviderKind]time.Time, len(rows))
	for _, row := range rows {
		if row.LastAcceptedAt.Valid {
			accepted[EMAProviderKind(row.EmaProvider)] = row.LastAcceptedAt.Time
		}
	}
	return StampEMAProviderLastAccepted(order, accepted), nil
}

// StampEMAProviderLastAccepted copies each provider kind's newest acceptance
// onto the order's providers of that kind. An external entry the endpoint
// does not trust is left blank: the acceptance was another issuer's.
func StampEMAProviderLastAccepted(order EMAProviderOrder, accepted map[EMAProviderKind]time.Time) EMAProviderOrder {
	order.Providers = append([]EMAProvider(nil), order.Providers...)
	for i, p := range order.Providers {
		at, ok := accepted[p.Kind]
		if !ok || p.Unavailable == EMAProviderNotTrustedByEndpoint {
			continue
		}
		order.Providers[i].LastAcceptedAt = &at
	}
	return order
}

// EMAProviderReadiness checks live signing eligibility for an organization
// issuer without minting. An empty reason means ready; an error means
// readiness is unknown.
type EMAProviderReadiness func(context.Context, string, uuid.UUID) (EMAProviderUnavailableReason, error)

// ApplyEMAProviderReadiness decorates a stored order for status views
// without requiring a user grant. A failed check marks the provider
// readiness_unknown rather than failing the read. Call after releasing
// transactions: readiness may query the database and flag providers.
func ApplyEMAProviderReadiness(ctx context.Context, organizationID string, order EMAProviderOrder, check EMAProviderReadiness) (EMAProviderOrder, error) {
	order.Providers = append([]EMAProvider(nil), order.Providers...)
	var errs []error
	for i, p := range order.Providers {
		if p.Kind != EMAProviderSpeakeasy || !p.Available() || check == nil {
			continue
		}
		reason, err := check(ctx, organizationID, p.InternalSessionIssuerID)
		if err != nil {
			order.Providers[i].Unavailable = EMAProviderReadinessUnknown
			errs = append(errs, fmt.Errorf("check issuer readiness: %w", err))
			continue
		}
		order.Providers[i].Unavailable = reason
	}
	return order, errors.Join(errs...)
}
