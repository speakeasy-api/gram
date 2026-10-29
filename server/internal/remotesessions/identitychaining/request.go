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

	// UserSessionJTI identifies the exact validated MCP grant. It never comes from caller input.
	UserSessionJTI string

	// UpstreamResource is the endpoint's upstream, compared to binding
	// resources without a trailing slash.
	UpstreamResource string

	// RemoteSessionIssuerID, when valid, restricts selection to bindings for
	// that issuer. Tunneled upstreams set their own derived issuer: their
	// resource identifier is operator supplied, so a resource match alone
	// could deliver a sibling upstream's token into the tunnel.
	RemoteSessionIssuerID uuid.NullUUID

	// RemoteSessionClientID, when valid, restricts selection to a binding for
	// that client: the client the caller's credential chose for the server.
	// A binding for any other client, even at the same resource, never serves
	// the caller.
	RemoteSessionClientID uuid.NullUUID

	// consentSignInID is the consent page's sign-in, set only by CheckConsent.
	// It puts the request in consent mode: the Speakeasy provider vouches for
	// this sign-in instead of a grant, and its credential is stored without
	// one. A request without it always needs UserSessionJTI.
	consentSignInID uuid.UUID

	// consentNeedsToken marks consent-time discovery, which calls the upstream
	// with the credential. A consent acceptance recorded without a stored
	// credential carries no token, so it never satisfies such a request.
	consentNeedsToken bool
}

// consent reports whether r checks a consent page's sign-in before its MCP
// grant exists.
func (r Request) consent() bool { return r.consentSignInID != uuid.Nil }

// complete reports whether every field needed to select a binding is set.
func (r Request) complete() bool {
	return r.OrganizationID != "" && r.ProjectID != uuid.Nil && r.UserSessionIssuerID != uuid.Nil && strings.TrimRight(r.UpstreamResource, "/") != ""
}

// NewRequest builds the request the MCP runtime makes for a human calling an
// upstream. A direct upstream selects by resource alone; a tunneled one is
// restricted to its own derived issuer and never chains without one.
func NewRequest(organizationID string, projectID, userSessionIssuerID uuid.UUID, userID, upstreamResource string, tunneled bool, tunneledIssuerID uuid.NullUUID) (Request, bool) {
	var none Request
	if userID == "" {
		return none, false
	}
	req, ok := NewUpstreamRequest(organizationID, projectID, userSessionIssuerID, upstreamResource, tunneled, tunneledIssuerID)
	if !ok {
		return none, false
	}
	req.UserID = userID
	return req, true
}

// NewUpstreamRequest is NewRequest without a human, for configuration reads
// that select a binding exactly as the runtime would.
func NewUpstreamRequest(organizationID string, projectID, userSessionIssuerID uuid.UUID, upstreamResource string, tunneled bool, tunneledIssuerID uuid.NullUUID) (Request, bool) {
	var none Request
	req := Request{
		OrganizationID:        organizationID,
		ProjectID:             projectID,
		UserSessionIssuerID:   userSessionIssuerID,
		UserID:                "",
		UserSessionJTI:        "",
		UpstreamResource:      upstreamResource,
		RemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		consentSignInID:       uuid.Nil,
		consentNeedsToken:     false,
	}
	if tunneled {
		if !tunneledIssuerID.Valid || tunneledIssuerID.UUID == uuid.Nil {
			return none, false
		}
		req.RemoteSessionIssuerID = tunneledIssuerID
	}
	if !req.complete() {
		return none, false
	}
	return req, true
}
