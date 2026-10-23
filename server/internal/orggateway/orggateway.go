// Package orggateway decides whether an organization is served an
// organization gateway: one MCP URL per organization, signed in to through
// the organization's own user session issuer, whose servers are every server
// in the organization the signed-in user can reach.
//
// The gateway is not stored. It exists while its organization owns exactly
// one organization-level user session issuer, so creating, moving, or
// deleting that issuer is what offers or withdraws it. An organization admin
// can also turn it off, which withdraws it until they turn it back on. It is
// served on the
// platform host only, so an organization whose custom domain restricts
// access to an IP allowlist, which the platform host enforces by refusing
// the organization's requests, is not served one.
package orggateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// issuerProbe bounds the issuer lookup. The gateway is offered only when the
// organization owns exactly one issuer, so two rows are enough to tell one
// from several.
const issuerProbe = 2

// Unavailability is why an organization is not served a gateway.
type Unavailability string

const (
	// UnavailabilityNone means the gateway is served.
	UnavailabilityNone Unavailability = ""

	// UnavailabilityNoOrganizationIssuer means the organization owns no
	// organization-level user session issuer for users to sign in through.
	UnavailabilityNoOrganizationIssuer Unavailability = "no_organization_issuer"

	// UnavailabilityMultipleOrganizationIssuers means the organization owns
	// several organization-level issuers, leaving nothing to choose between
	// them by.
	UnavailabilityMultipleOrganizationIssuers Unavailability = "multiple_organization_issuers"

	// UnavailabilityCustomDomainOnly means the organization's custom domain
	// restricts access to an IP allowlist, so the platform host the gateway
	// is served on refuses the organization's requests.
	UnavailabilityCustomDomainOnly Unavailability = "custom_domain_only"

	// UnavailabilityOrganizationDisabled means the organization is disabled.
	UnavailabilityOrganizationDisabled Unavailability = "organization_disabled"

	// UnavailabilityTurnedOff means an organization admin turned the gateway
	// off.
	UnavailabilityTurnedOff Unavailability = "turned_off"
)

// Availability returns the issuer an organization's gateway signs users in
// through, or why the organization is not served one. Every surface that
// offers the gateway asks this, so none offers a URL the gateway refuses.
// Every read is uncached, so turning the gateway off takes effect on the next
// request.
func Availability(ctx context.Context, db usersessionsrepo.DBTX, organization orgrepo.OrganizationMetadatum) (uuid.UUID, Unavailability, error) {
	if organization.DisabledAt.Valid {
		return uuid.Nil, UnavailabilityOrganizationDisabled, nil
	}
	turnedOff, err := featuresrepo.New(db).IsFeatureEnabled(ctx, featuresrepo.IsFeatureEnabledParams{
		OrganizationID: organization.ID,
		FeatureName:    string(productfeatures.FeatureOrganizationGatewayDisabled),
	})
	if err != nil {
		return uuid.Nil, UnavailabilityNone, fmt.Errorf("read organization gateway setting: %w", err)
	}
	if turnedOff {
		return uuid.Nil, UnavailabilityTurnedOff, nil
	}
	domain, err := customdomainsrepo.New(db).GetCustomDomainByOrganization(ctx, organization.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return uuid.Nil, UnavailabilityNone, fmt.Errorf("load custom domain: %w", err)
	case len(domain.IpAllowlist) > 0:
		return uuid.Nil, UnavailabilityCustomDomainOnly, nil
	}
	return Resolve(ctx, usersessionsrepo.New(db), organization.ID)
}

// IssuerLister lists an organization's organization-level user session
// issuers; usersessions/repo.Queries implements it.
type IssuerLister interface {
	ListOrganizationUserSessionIssuers(ctx context.Context, arg usersessionsrepo.ListOrganizationUserSessionIssuersParams) ([]usersessionsrepo.ListOrganizationUserSessionIssuersRow, error)
}

// Resolve returns the issuer an organization's gateway signs users in
// through, or why the organization is not served one.
func Resolve(ctx context.Context, issuers IssuerLister, organizationID string) (uuid.UUID, Unavailability, error) {
	rows, err := issuers.ListOrganizationUserSessionIssuers(ctx, usersessionsrepo.ListOrganizationUserSessionIssuersParams{
		IncludeOrganizational:  true,
		OrganizationID:         organizationID,
		IncludeProjectSpecific: false,
		Cursor:                 uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:             issuerProbe,
	})
	if err != nil {
		return uuid.Nil, UnavailabilityNone, fmt.Errorf("list organization user session issuers: %w", err)
	}
	switch len(rows) {
	case 0:
		return uuid.Nil, UnavailabilityNoOrganizationIssuer, nil
	case 1:
		return rows[0].UserSessionIssuer.ID, UnavailabilityNone, nil
	default:
		return uuid.Nil, UnavailabilityMultipleOrganizationIssuers, nil
	}
}

// URL is the organization gateway's MCP URL under baseURL.
func URL(baseURL *url.URL, organizationSlug string) string {
	return baseURL.JoinPath(mcproute.OrganizationGateway.String(), organizationSlug).String()
}
