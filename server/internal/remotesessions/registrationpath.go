package remotesessions

import (
	"slices"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// RegistrationPath is how a client is obtained from an authorization server
// without credentials entered by hand.
type RegistrationPath string

const (
	// RegistrationPathManual means neither automatic path applies, so a
	// client must be configured by hand.
	RegistrationPathManual RegistrationPath = "manual"

	// RegistrationPathDCR is RFC 7591 dynamic client registration.
	RegistrationPathDCR RegistrationPath = "dynamic_client_registration"

	// RegistrationPathCIMD is a Gram-hosted Client ID Metadata Document.
	RegistrationPathCIMD RegistrationPath = "client_id_metadata_document"
)

// RegistrationOrder is which automatic registration paths a policy allows, in
// the order it tries them.
type RegistrationOrder string

const (
	// RegistrationOrderCIMDFirst uses a Client ID Metadata Document when the
	// provider supports one, and dynamic registration otherwise.
	RegistrationOrderCIMDFirst RegistrationOrder = "cimd_first"

	// RegistrationOrderDCRFirst uses dynamic registration when the policy can
	// use the provider's endpoint, and a Client ID Metadata Document
	// otherwise.
	RegistrationOrderDCRFirst RegistrationOrder = "dcr_first"

	// RegistrationOrderDCROnly never uses a Client ID Metadata Document. An
	// unrecognized order, including the zero value, behaves the same way.
	RegistrationOrderDCROnly RegistrationOrder = "dcr_only"
)

// RegistrationCapabilities is what an authorization server's RFC 8414 metadata
// says about obtaining a client from it. The JSON tags are the metadata
// members, so a metadata document decodes into it directly.
type RegistrationCapabilities struct {
	// RegistrationEndpoint is the RFC 7591 dynamic client registration
	// endpoint, or "" when the server advertises none.
	RegistrationEndpoint string `json:"registration_endpoint"`

	// TokenEndpointAuthMethodsSupported lists the client authentication
	// methods the token endpoint accepts. An empty list means the server did
	// not enumerate them.
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`

	// ClientIDMetadataDocumentSupported is the CIMD draft's
	// client_id_metadata_document_supported flag.
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported"`
}

// RegistrationCapabilities is the registration subset of the discovered
// metadata.
func (m DiscoveredIssuerMetadata) RegistrationCapabilities() RegistrationCapabilities {
	return RegistrationCapabilities{
		RegistrationEndpoint:              m.RegistrationEndpoint,
		TokenEndpointAuthMethodsSupported: m.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: m.ClientIDMetadataDocumentSupported,
	}
}

// IssuerRegistrationCapabilities is the registration subset of a stored
// provider; a NULL registration endpoint is none.
func IssuerRegistrationCapabilities(issuer repo.RemoteSessionIssuer) RegistrationCapabilities {
	return RegistrationCapabilities{
		RegistrationEndpoint:              issuer.RegistrationEndpoint.String,
		TokenEndpointAuthMethodsSupported: issuer.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: issuer.ClientIDMetadataDocumentSupported,
	}
}

// ChooseRegistration is the automatic registration path policy takes for a
// provider with capabilities. Every caller that chooses between dynamic
// registration, a Client ID Metadata Document and manual setup uses it, so
// what a caller reports matches what a commit with the same policy does.
//
// Dynamic registration applies when the endpoint is an absolute https URL
// (or http on loopback, when the policy allows it) and, for a policy that
// requires a confidential client using a named method, the token endpoint
// accepts that method or does not enumerate its methods. A Client ID Metadata
// Document applies under SupportsClientIDMetadataDocument. The policy's Order
// decides between them when both apply.
func ChooseRegistration(capabilities RegistrationCapabilities, policy RegistrationPolicy) RegistrationPath {
	dcr := policy.canUseDynamicRegistration(capabilities)
	cimd := (policy.Order == RegistrationOrderCIMDFirst || policy.Order == RegistrationOrderDCRFirst) &&
		SupportsClientIDMetadataDocument(capabilities.ClientIDMetadataDocumentSupported, capabilities.TokenEndpointAuthMethodsSupported)
	switch {
	case cimd && policy.Order == RegistrationOrderCIMDFirst:
		return RegistrationPathCIMD
	case dcr:
		return RegistrationPathDCR
	case cimd:
		return RegistrationPathCIMD
	default:
		return RegistrationPathManual
	}
}

// registrationEndpointAllowed reports whether the policy may send a dynamic
// registration request to endpoint.
func (p RegistrationPolicy) registrationEndpointAllowed(endpoint string) bool {
	if p.AllowLoopbackRegistrationEndpoint {
		return urls.IsAbsoluteHTTPSOrLoopback(endpoint)
	}
	return urls.IsAbsoluteHTTPS(endpoint)
}

// canUseDynamicRegistration reports whether dynamic registration can give the
// policy a client it accepts. A token endpoint that does not enumerate its
// methods defaults to client_secret_basic under RFC 8414, so it is not
// second-guessed.
func (p RegistrationPolicy) canUseDynamicRegistration(capabilities RegistrationCapabilities) bool {
	if !p.registrationEndpointAllowed(capabilities.RegistrationEndpoint) {
		return false
	}
	if !p.RequireClientSecret || p.TokenEndpointAuthMethod == nil {
		return true
	}
	methods := capabilities.TokenEndpointAuthMethodsSupported
	return len(methods) == 0 || slices.Contains(methods, *p.TokenEndpointAuthMethod)
}
