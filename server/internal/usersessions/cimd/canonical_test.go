package cimd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCanonicalJSONWithholdsTheLogoURI pins that the operator-facing render
// does not carry logo_uri.
//
// Document.LogoURI is documented as deliberately not rendered: it is
// attacker-controlled, nothing acts on it, and putting an attacker-chosen URL
// in front of the operator deciding whether to trust the client is the whole
// problem. client_name is attacker-controlled as well and IS rendered, because
// it is the name the decision is about, so this asserts both directions rather
// than just the omission.
func TestCanonicalJSONWithholdsTheLogoURI(t *testing.T) {
	t.Parallel()

	document := &Document{
		ClientID:                "https://client.example/client.json",
		ClientName:              "Example Client",
		ClientSecret:            nil,
		ClientSecretExpiresAt:   nil,
		ClientURI:               "https://client.example",
		GrantTypes:              []string{"authorization_code"},
		JWKS:                    nil,
		JWKSURI:                 "",
		LogoURI:                 "https://attacker.example/logo.png",
		RedirectURIs:            []string{"http://127.0.0.1:3000/callback"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
	}

	rendered, err := CanonicalJSON(document)
	require.NoError(t, err)

	var members map[string]any
	require.NoError(t, json.Unmarshal([]byte(rendered), &members))

	require.NotContains(t, members, "logo_uri", "logo_uri must not reach the operator view")
	require.NotContains(t, rendered, "attacker.example", "the logo host must not appear anywhere in the render")

	require.Equal(t, "Example Client", members["client_name"], "the name the decision is about must still be shown")
	require.Equal(t, "https://client.example", members["client_uri"])
	require.Equal(t, "https://client.example/client.json", members["client_id"])
}
