package remotesessions

import (
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// TokenEndpointRegistration is a stored remote session client registration and
// the issuer token endpoint it authenticates at.
type TokenEndpointRegistration struct {
	// ClientID is the remote_session_clients row.
	ClientID uuid.UUID

	// OrganizationID owns the client; private_key_jwt signs with its keys,
	// and its transition state decides which secret copy is read. Empty for a
	// client row without an organization.
	OrganizationID string

	// ExternalClientID is the client_id the authorization server issued.
	ExternalClientID string

	// ClientSecret is the client secret's stored copies, both NULL when the
	// client has none.
	ClientSecret orgencryption.Copies

	// TokenEndpointAuthMethod is the stored token_endpoint_auth_method.
	TokenEndpointAuthMethod string

	// TokenEndpointAuthAudienceFormat is the stored client assertion audience
	// format; empty means the issuer identifier.
	TokenEndpointAuthAudienceFormat string

	// JSONWebKeySetID is the key set private_key_jwt signs with.
	JSONWebKeySetID uuid.NullUUID

	// IssuerID is the remote_session_issuers row.
	IssuerID uuid.UUID

	// IssuerURL is the authorization server's issuer identifier.
	IssuerURL string

	// IssuerMetadata is the issuer's stored authorization server metadata.
	IssuerMetadata []byte

	// TokenEndpoint is the issuer's token endpoint URL.
	TokenEndpoint string

	// TunneledMcpServerID routes the issuer's egress through a tunnel when set.
	TunneledMcpServerID uuid.NullUUID
}
