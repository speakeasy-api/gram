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

// The shape an issuer that was hand-configured, or created before capture
// existed, actually has: nullable PKCE column NULL, optional endpoints empty,
// capability arrays empty. Every other reconstruction test seeds them non-nil,
// so this is the one that sees what a real degraded row serves.
//
// Emitting these would break clients rather than merely under-inform them:
// `"code_challenge_methods_supported": null` fails an optional-array schema
// outright, `"registration_endpoint": ""` is not the URL RFC 8414 promises,
// and `"response_types_supported": []` asserts the server supports no response
// types at all, so a strict client refuses `code`.
func TestResolveOAuthServerMetadataFromRemoteSessionIssuer_OmitsUnknownMembers(t *testing.T) {
	t.Parallel()

	result, fromSnapshot, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
		AuthorizationEndpoint:             "https://idp.example.com/authorize",
		TokenEndpoint:                     "https://idp.example.com/token",
		RegistrationEndpoint:              "",
		RevocationEndpoint:                "",
		JwksURI:                           "",
		ScopesSupported:                   []string{},
		ResponseTypesSupported:            []string{},
		GrantTypesSupported:               []string{},
		TokenEndpointAuthMethodsSupported: []string{},
		CodeChallengeMethodsSupported:     nil,
		Snapshot:                          nil,
	}, upstreamResourceURL)
	require.NoError(t, err)
	require.False(t, fromSnapshot)

	encoded, err := json.Marshal(result.Static)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(encoded, &got))

	// The two members RFC 8414 always requires, plus the rewritten issuer.
	require.Equal(t, map[string]any{
		"issuer":                 upstreamResourceURL,
		"authorization_endpoint": "https://idp.example.com/authorize",
		"token_endpoint":         "https://idp.example.com/token",
	}, got, "an unknown member must be absent, not null or empty")
}

// The counterpart: a member Gram does know is served, including the three the
// first version of this reconstruction dropped despite modelling them.
func TestResolveOAuthServerMetadataFromRemoteSessionIssuer_ServesEveryModelledMember(t *testing.T) {
	t.Parallel()

	result, _, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
		AuthorizationEndpoint:             "https://idp.example.com/authorize",
		TokenEndpoint:                     "https://idp.example.com/token",
		RegistrationEndpoint:              "https://idp.example.com/register",
		RevocationEndpoint:                "https://idp.example.com/revoke",
		JwksURI:                           "https://idp.example.com/jwks",
		ScopesSupported:                   []string{"openid"},
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		Snapshot:                          nil,
	}, upstreamResourceURL)
	require.NoError(t, err)

	require.Equal(t, "https://idp.example.com/revoke", result.Static.RevocationEndpoint)
	require.Equal(t, "https://idp.example.com/jwks", result.Static.JwksURI)
	// RFC 8414 reads an absent value here as client_secret_basic, so dropping
	// it would break a public client that authenticates with `none`.
	require.Equal(t, []string{"none"}, result.Static.TokenEndpointAuthMethodsSupported)
}

// An issuer with neither a snapshot nor endpoints has nothing to advertise.
// Serving 200 with empty required members would hand a client a document it
// cannot start a flow from and no signal that the server knows it.
func TestResolveOAuthServerMetadataFromRemoteSessionIssuer_RefusesIncompleteReconstruction(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		authorization string
		token         string
	}{
		{name: "no endpoints at all", authorization: "", token: ""},
		{name: "no token endpoint", authorization: "https://idp.example.com/authorize", token: ""},
		{name: "no authorization endpoint", authorization: "", token: "https://idp.example.com/token"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
				AuthorizationEndpoint: tt.authorization,
				TokenEndpoint:         tt.token,
				Snapshot:              nil,
			}, upstreamResourceURL)
			require.ErrorIs(t, err, ErrIncompleteIssuerMetadata)
		})
	}
}

// The documentation and CIMD members the first reconstruction dropped despite
// remote_session_issuers modelling them.
func TestResolveOAuthServerMetadataFromRemoteSessionIssuer_ServesDocumentationAndCIMD(t *testing.T) {
	t.Parallel()

	result, _, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
		AuthorizationEndpoint:             "https://idp.example.com/authorize",
		TokenEndpoint:                     "https://idp.example.com/token",
		ServiceDocumentation:              "https://idp.example.com/docs",
		OpPolicyURI:                       "https://idp.example.com/policy",
		OpTosURI:                          "https://idp.example.com/tos",
		ClientIDMetadataDocumentSupported: true,
		Snapshot:                          nil,
	}, upstreamResourceURL)
	require.NoError(t, err)

	require.Equal(t, "https://idp.example.com/docs", result.Static.ServiceDocumentation)
	require.Equal(t, "https://idp.example.com/policy", result.Static.OpPolicyURI)
	require.Equal(t, "https://idp.example.com/tos", result.Static.OpTosURI)
	require.NotNil(t, result.Static.ClientIDMetadataDocumentSupported)
	require.True(t, *result.Static.ClientIDMetadataDocumentSupported)

	// An issuer that does not support CIMD omits the member rather than
	// advertising false, which means the same thing and is noise.
	plain, _, err := ResolveOAuthServerMetadataFromRemoteSessionIssuer(RemoteSessionIssuerMetadata{
		AuthorizationEndpoint: "https://idp.example.com/authorize",
		TokenEndpoint:         "https://idp.example.com/token",
		Snapshot:              nil,
	}, upstreamResourceURL)
	require.NoError(t, err)
	encoded, err := json.Marshal(plain.Static)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "client_id_metadata_document_supported")
}
