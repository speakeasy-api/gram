// Package orggateway decides whether an organization is served an
// organization gateway: one MCP URL per organization, signed in to through
// the organization's own user session issuer, whose servers are every server
// in the organization the signed-in user can reach.
//
// The gateway is not stored. It exists while its organization owns exactly
// one organization-level user session issuer, so creating, moving, or
// deleting that issuer is what offers or withdraws it.
package orggateway

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"

	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// RouteBase is the URL path prefix organization gateways are served under,
// followed by the organization's slug.
const RouteBase = "mcp/gateway"

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
)

// IssuerLister lists an organization's organization-level user session
// issuers; usersessions/repo.Queries implements it.
type IssuerLister interface {
	ListOrganizationUserSessionIssuers(ctx context.Context, arg usersessionsrepo.ListOrganizationUserSessionIssuersParams) ([]usersessionsrepo.UserSessionIssuer, error)
}

// Resolve returns the issuer an organization's gateway signs users in
// through, or why the organization is not served one.
func Resolve(ctx context.Context, issuers IssuerLister, organizationID string) (uuid.UUID, Unavailability, error) {
	rows, err := issuers.ListOrganizationUserSessionIssuers(ctx, usersessionsrepo.ListOrganizationUserSessionIssuersParams{
		OrganizationID: organizationID,
		Cursor:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:     issuerProbe,
	})
	if err != nil {
		return uuid.Nil, UnavailabilityNone, fmt.Errorf("list organization user session issuers: %w", err)
	}
	switch len(rows) {
	case 0:
		return uuid.Nil, UnavailabilityNoOrganizationIssuer, nil
	case 1:
		return rows[0].ID, UnavailabilityNone, nil
	default:
		return uuid.Nil, UnavailabilityMultipleOrganizationIssuers, nil
	}
}

// URL is the organization gateway's MCP URL under baseURL.
func URL(baseURL *url.URL, organizationSlug string) string {
	return baseURL.JoinPath(RouteBase, organizationSlug).String()
}
