package cimd

import (
	"encoding/json"
	"fmt"
)

// CanonicalJSON renders a validated document for display to an operator.
//
// Re-encoded from the parsed document rather than echoed from the wire: what
// matters is what the authorization server will act on, not the bytes served.
// Unset members are omitted so absence reads as absence — notably
// token_endpoint_auth_method, where absent resolves to "none".
func CanonicalJSON(document *Document) (string, error) {
	if document == nil {
		return "", fmt.Errorf("cimd: no document to render")
	}

	members := map[string]any{
		"client_id":     document.ClientID,
		"client_name":   document.ClientName,
		"redirect_uris": document.RedirectURIs,
	}
	setString := func(key, value string) {
		if value != "" {
			members[key] = value
		}
	}
	setList := func(key string, value []string) {
		if len(value) > 0 {
			members[key] = value
		}
	}
	setString("client_uri", document.ClientURI)
	setString("logo_uri", document.LogoURI)
	setString("jwks_uri", document.JWKSURI)
	setString("token_endpoint_auth_method", document.TokenEndpointAuthMethod)
	setList("grant_types", document.GrantTypes)
	setList("response_types", document.ResponseTypes)
	if len(document.JWKS) > 0 {
		members["jwks"] = document.JWKS
	}

	encoded, err := json.MarshalIndent(members, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cimd: render document: %w", err)
	}
	return string(encoded), nil
}
