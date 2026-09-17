package idjag

import "github.com/google/uuid"

// Request contains trusted endpoint and authenticated-client values supplied
// by the grant handler; none is taken from the unverified JWT.
type Request struct {
	// OrganizationID identifies the organization accepting the assertion.
	OrganizationID string
	// UserSessionIssuerID identifies the configured authorization server.
	UserSessionIssuerID uuid.UUID
	// Audience is the authorization server issuer expected by the assertion.
	Audience string
	// Resource is the MCP server expected by the assertion.
	Resource string
	// ClientID is the authenticated client expected by the assertion.
	ClientID string
}
