// Package orgserving decides whether Speakeasy serves an organization's MCP
// servers at all, before any per-server, per-gateway, or per-caller policy.
package orgserving

import (
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// Refusal is why an organization's MCP servers are not served, or
// RefusalNone when they are.
type Refusal string

const (
	// RefusalNone means the organization's MCP servers are served.
	RefusalNone Refusal = ""

	// RefusalDisabled means the organization is disabled.
	RefusalDisabled Refusal = "organization_disabled"

	// RefusalNotWhitelisted means the organization has not been granted access
	// to Speakeasy: it never was, or it lost access, for example when a trial
	// was demoted.
	RefusalNotWhitelisted Refusal = "organization_not_whitelisted"
)

// Check reports why the MCP gateway must not serve organization's MCP
// servers. Callers must read organization from the database for the request
// they are serving, so a change to either flag takes effect on the next
// request.
func Check(organization orgrepo.OrganizationMetadatum) Refusal {
	switch {
	case organization.DisabledAt.Valid:
		return RefusalDisabled
	case !organization.Whitelisted:
		return RefusalNotWhitelisted
	default:
		return RefusalNone
	}
}
