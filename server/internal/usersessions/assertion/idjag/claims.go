package idjag

import "time"

// Claims are the verified ID-JAG values needed by a grant handler.
type Claims struct {
	// Issuer identifies the assertion issuer.
	Issuer string
	// ExternalSubject identifies the user at the assertion issuer.
	ExternalSubject string
	// Email is the verified identity used for subject resolution.
	Email string
	// Resource identifies the MCP server named by the assertion.
	Resource string
	// ClientID identifies the client named by the assertion.
	ClientID string
	// JTI is the assertion identifier reserved against replay.
	JTI string
	// Scope is the scope requested by the assertion.
	Scope string
	// ExpiresAt is the assertion expiration time.
	ExpiresAt time.Time
}
