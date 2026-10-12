package identitychaining

import (
	"strings"

	"github.com/google/uuid"
)

// Request identifies an authorized, provisioned human at one endpoint.
// Callers derive every field from the authenticated request context, never
// from caller-supplied provenance.
type Request struct {
	// OrganizationID is the endpoint's organization.
	OrganizationID string

	// ProjectID is the endpoint's project.
	ProjectID uuid.UUID

	// UserSessionIssuerID is the endpoint's user session issuer.
	UserSessionIssuerID uuid.UUID

	// UserID is the authenticated Speakeasy human, never an agent or workload.
	UserID string

	// UpstreamResource is the endpoint's upstream, compared to binding
	// resources without a trailing slash.
	UpstreamResource string

	// RemoteSessionIssuerID, when valid, restricts selection to bindings for
	// that issuer. Tunneled upstreams set their own derived issuer: their
	// resource identifier is operator supplied, so a resource match alone
	// could deliver a sibling upstream's token into the tunnel.
	RemoteSessionIssuerID uuid.NullUUID
}

// complete reports whether every field needed to select a binding is set.
func (r Request) complete() bool {
	return r.OrganizationID != "" && r.ProjectID != uuid.Nil && r.UserSessionIssuerID != uuid.Nil && strings.TrimRight(r.UpstreamResource, "/") != ""
}
