package wellknown

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const upstreamResourceURL = "https://app.getgram.ai/mcp/example"

// The snapshot path must behave exactly like the toolset external-OAuth branch
// it replaces: re-serve the captured document with only `issuer` rewritten, so
// upstream extension fields survive.
func TestResolveOAuthServerMetadataFromRemoteSessionIssuer_FromSnapshot(t *testing.T) {
	t.Parallel()

	snapshot := json.RawMessage(`{
		"issuer": "https://idp.example.com",
		"authorization_endpoint": "https://idp.example.com/authorize",
		"token_endpoint": "https://idp.example.com/token",
		"userinfo_endpoint": "https://idp.example.com/userinfo",
		"claims_supported": ["sub", "email"],
		"id_token_signing_alg_values_supported": ["RS256"]
	}`)

	result, fromSnapshot, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
		AuthorizationEndpoint:         "https://idp.example.com/authorize",
		TokenEndpoint:                 "https://idp.example.com/token",
		RegistrationEndpoint:          "",
		ScopesSupported:               nil,
		ResponseTypesSupported:        nil,
		GrantTypesSupported:           nil,
		CodeChallengeMethodsSupported: nil,
		Snapshot:                      snapshot,
	}, upstreamResourceURL)
	require.NoError(t, err)
	require.True(t, fromSnapshot)
	require.Equal(t, OAuthServerMetadataResultKindRaw, result.Kind)

	var got map[string]any
	require.NoError(t, json.Unmarshal(result.Raw, &got))

	// RFC 8414 3.3: the served issuer must equal the URL the client fetched the
	// document from, which is Gram's, not the upstream's.
	require.Equal(t, upstreamResourceURL, got["issuer"])

	// The upstream's own endpoints are carried through unchanged. The RFC does
	// not require them to share the issuer's origin, and rewriting them would
	// send the client to Gram for a flow Gram does not host.
	require.Equal(t, "https://idp.example.com/authorize", got["authorization_endpoint"])
	require.Equal(t, "https://idp.example.com/token", got["token_endpoint"])

	// The whole reason the snapshot exists: fields the typed columns do not
	// model survive into the served document.
	require.Equal(t, "https://idp.example.com/userinfo", got["userinfo_endpoint"])
	require.Equal(t, []any{"sub", "email"}, got["claims_supported"])
	require.Equal(t, []any{"RS256"}, got["id_token_signing_alg_values_supported"])
}

// An issuer that predates capture, or was configured by hand, still serves a
// usable document. It is missing only the extension fields, and refreshing the
// issuer resolves that.
func TestResolveOAuthServerMetadataFromRemoteSessionIssuer_ReconstructsWithoutSnapshot(t *testing.T) {
	t.Parallel()

	result, fromSnapshot, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
		AuthorizationEndpoint:         "https://idp.example.com/authorize",
		TokenEndpoint:                 "https://idp.example.com/token",
		RegistrationEndpoint:          "https://idp.example.com/register",
		ScopesSupported:               []string{"openid"},
		ResponseTypesSupported:        []string{"code"},
		GrantTypesSupported:           []string{"authorization_code"},
		CodeChallengeMethodsSupported: []string{"S256"},
		Snapshot:                      nil,
	}, upstreamResourceURL)
	require.NoError(t, err)
	require.False(t, fromSnapshot, "the caller logs a warning on this path")
	require.Equal(t, OAuthServerMetadataResultKindStatic, result.Kind)

	require.Equal(t, upstreamResourceURL, result.Static.Issuer)
	require.Equal(t, "https://idp.example.com/authorize", result.Static.AuthorizationEndpoint)
	require.Equal(t, "https://idp.example.com/token", result.Static.TokenEndpoint)
	require.Equal(t, "https://idp.example.com/register", result.Static.RegistrationEndpoint)
	require.Equal(t, []string{"S256"}, result.Static.CodeChallengeMethodsSupported)
}

// An empty JSON object is a captured snapshot, not an absent one, so it takes
// the snapshot path rather than silently reconstructing.
func TestResolveOAuthServerMetadataFromRemoteSessionIssuer_EmptyObjectIsASnapshot(t *testing.T) {
	t.Parallel()

	result, fromSnapshot, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
		AuthorizationEndpoint:         "https://idp.example.com/authorize",
		TokenEndpoint:                 "https://idp.example.com/token",
		RegistrationEndpoint:          "",
		ScopesSupported:               nil,
		ResponseTypesSupported:        nil,
		GrantTypesSupported:           nil,
		CodeChallengeMethodsSupported: nil,
		Snapshot:                      json.RawMessage(`{}`),
	}, upstreamResourceURL)
	require.NoError(t, err)
	require.True(t, fromSnapshot)

	var got map[string]any
	require.NoError(t, json.Unmarshal(result.Raw, &got))
	require.Equal(t, map[string]any{"issuer": upstreamResourceURL}, got)
}

func TestResolveOAuthServerMetadataFromRemoteSessionIssuer_RejectsUnparseableSnapshot(t *testing.T) {
	t.Parallel()

	_, _, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
		AuthorizationEndpoint:         "https://idp.example.com/authorize",
		TokenEndpoint:                 "https://idp.example.com/token",
		RegistrationEndpoint:          "",
		ScopesSupported:               nil,
		ResponseTypesSupported:        nil,
		GrantTypesSupported:           nil,
		CodeChallengeMethodsSupported: nil,
		Snapshot:                      json.RawMessage(`["not an object"]`),
	}, upstreamResourceURL)
	require.ErrorContains(t, err, "rewrite remote session issuer metadata")
}
