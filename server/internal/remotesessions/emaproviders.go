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

// EMAProviderUnavailableReason says why the selected provider cannot serve.
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

// EMAProvider is a vendor's EMA provider. An external provider with no
// remote session issuer stands for the endpoint's trusted issuer.
type EMAProvider struct {
	Kind                    EMAProviderKind
	RemoteSessionIssuerID   uuid.UUID
	InternalSessionIssuerID uuid.UUID
	Unavailable             EMAProviderUnavailableReason
}

// Available reports whether the provider can serve now.
func (p EMAProvider) Available() bool { return p.Unavailable == "" }

// EMAProviderSelection is a vendor's EMA provider, selected or default.
type EMAProviderSelection struct {
	VendorIssuer string
	IsDefault    bool
	Provider     EMAProvider
}

// EMAProviderScope is what a selection is resolved for.
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

// NormalizeVendorIssuer keys a vendor's EMA provider by its issuer, in the
// same canonical form issuer lookups use.
func NormalizeVendorIssuer(issuer string) (string, error) {
	canonical, err := issuerurl.Parse(issuer)
	if err != nil {
		return "", ErrInvalidVendorIssuer
	}
	return canonical.String(), nil
}

// ResolveEMAProvider reads the explicitly selected provider for a vendor.
// No selection keeps the endpoint's external issuer; enabling the Speakeasy
// issuer alone never changes an existing integration's authority.
func ResolveEMAProvider(ctx context.Context, q *repo.Queries, organizationID, vendorIssuer string, scope EMAProviderScope) (EMAProviderSelection, error) {
	selection := EMAProviderSelection{VendorIssuer: vendorIssuer, IsDefault: true, Provider: EMAProvider{Kind: EMAProviderExternal, RemoteSessionIssuerID: scope.TrustedIssuer.UUID, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}}
	rows, err := q.ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: organizationID, VendorIssuer: vendorIssuer})
	switch {
	case err != nil:
		return selection, fmt.Errorf("list ema providers: %w", err)
	case len(rows) == 0:
		return selection, nil
	case len(rows) > 1:
		// Writes save at most one provider; never pick one of several.
		return selection, fmt.Errorf("vendor %s has %d saved ema providers", vendorIssuer, len(rows))
	}
	row := rows[0]
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
	return EMAProviderSelection{VendorIssuer: vendorIssuer, IsDefault: false, Provider: p}, nil
}

// EndpointEMAProvider resolves vendorIssuer's provider for one endpoint, whose
// external provider is the user session issuer's trusted issuer. The
// management API and identity chaining both read the selection through it.
func EndpointEMAProvider(ctx context.Context, q *repo.Queries, organizationID string, userSessionIssuerID uuid.UUID, vendorIssuer string, speakeasySigns bool) (EMAProviderSelection, error) {
	trusted := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	issuer, err := q.GetEMAChainingUserIssuer(ctx, repo.GetEMAChainingUserIssuerParams{ID: userSessionIssuerID, OrganizationID: conv.ToPGText(organizationID)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return EMAProviderSelection{VendorIssuer: vendorIssuer, IsDefault: false, Provider: EMAProvider{Kind: "", RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}}, fmt.Errorf("read identity chaining user session issuer: %w", err)
	default:
		trusted = issuer.TrustedRemoteSessionIssuerID
	}
	return ResolveEMAProvider(ctx, q, organizationID, vendorIssuer, EMAProviderScope{Endpoint: true, TrustedIssuer: trusted, SpeakeasySigns: speakeasySigns})
}

// EMAProviderReadiness checks live signing eligibility for an organization
// issuer without minting. An empty reason means ready; an error means
// readiness is unknown.
type EMAProviderReadiness func(context.Context, string, uuid.UUID) (EMAProviderUnavailableReason, error)

// ApplyEMAProviderReadiness decorates a stored selection for status views
// without requiring a user grant. A failed check marks the provider
// readiness_unknown rather than failing the read. Call after releasing
// transactions: readiness may query the database and flag providers.
func ApplyEMAProviderReadiness(ctx context.Context, organizationID string, selection EMAProviderSelection, check EMAProviderReadiness) (EMAProviderSelection, error) {
	p := selection.Provider
	if p.Kind != EMAProviderSpeakeasy || !p.Available() || check == nil {
		return selection, nil
	}
	reason, err := check(ctx, organizationID, p.InternalSessionIssuerID)
	if err != nil {
		selection.Provider.Unavailable = EMAProviderReadinessUnknown
		return selection, fmt.Errorf("check issuer readiness: %w", err)
	}
	selection.Provider.Unavailable = reason
	return selection, nil
}
