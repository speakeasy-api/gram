package remotesessions

import "github.com/google/uuid"

// TokenEndpointRegistration is a stored remote session client registration and
// the issuer token endpoint it authenticates at.
type TokenEndpointRegistration struct {
	// ClientID is the remote_session_clients row.
	ClientID uuid.UUID

	// OrganizationID owns the client; private_key_jwt signs with its keys.
	OrganizationID string

	// ExternalClientID is the client_id the authorization server issued.
	ExternalClientID string

	// ClientSecretEncrypted is the encrypted client secret, or empty.
	ClientSecretEncrypted string

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
