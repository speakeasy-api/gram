package cimd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// testDocument returns a document that passes every validateDocument check
// for the given client_id URL: same-origin plus loopback redirect URIs,
// public auth method, required client_name.
func testDocument(clientID string) *Document {
	return &Document{
		ClientID:                clientID,
		ClientName:              "Test Client",
		ClientSecret:            nil,
		ClientSecretExpiresAt:   nil,
		ClientURI:               "",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		JWKS:                    nil,
		LogoURI:                 "",
		RedirectURIs:            []string{"https://client.example.com/callback", "http://127.0.0.1:3000/callback"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
	}
}

const testClientID = "https://client.example.com/oauth/client.json"

func requireOAuthError(t *testing.T, err error, code string) {
	t.Helper()
	var oauthErr *usersessions.OAuthError
	require.ErrorAs(t, err, &oauthErr)
	require.Equal(t, code, oauthErr.Code)
}

func TestValidateClientIDURL_Valid(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)
	require.Equal(t, "client.example.com", parsed.Host)
	require.Equal(t, "/oauth/client.json", parsed.Path)
}

func TestValidateClientIDURL_QueryTolerated(t *testing.T) {
	t.Parallel()

	_, err := validateClientIDURL("https://client.example.com/oauth/client.json?v=2")
	require.NoError(t, err)
}

func TestValidateClientIDURL_MissingPathRejected(t *testing.T) {
	t.Parallel()

	_, err := validateClientIDURL("https://client.example.com")
	requireOAuthError(t, err, "invalid_request")
}

func TestValidateClientIDURL_UserinfoRejected(t *testing.T) {
	t.Parallel()

	_, err := validateClientIDURL("https://user@client.example.com/client.json")
	requireOAuthError(t, err, "invalid_request")
}

func TestValidateClientIDURL_FragmentRejected(t *testing.T) {
	t.Parallel()

	_, err := validateClientIDURL("https://client.example.com/client.json#frag")
	requireOAuthError(t, err, "invalid_request")
}

func TestValidateClientIDURL_DotDotSegmentRejected(t *testing.T) {
	t.Parallel()

	_, err := validateClientIDURL("https://client.example.com/oauth/../client.json")
	requireOAuthError(t, err, "invalid_request")
}

func TestValidateClientIDURL_DotSegmentRejected(t *testing.T) {
	t.Parallel()

	_, err := validateClientIDURL("https://client.example.com/./client.json")
	requireOAuthError(t, err, "invalid_request")
}

func TestValidateClientIDURL_NonHTTPSRejected(t *testing.T) {
	t.Parallel()

	_, err := validateClientIDURL("http://client.example.com/client.json")
	requireOAuthError(t, err, "invalid_request")
}

func TestValidateClientIDURL_OversizedRejected(t *testing.T) {
	t.Parallel()

	long := "https://client.example.com/" + strings.Repeat("a", maxClientIDLength)
	_, err := validateClientIDURL(long)
	requireOAuthError(t, err, "invalid_request")
}

func TestValidateDocument_Valid(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)
	require.NoError(t, validateDocument(testDocument(testClientID), testClientID, parsed))
}

func TestValidateDocument_ClientIDMismatchRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientID = "https://client.example.com/oauth/other.json"
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_MissingClientNameRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientName = ""
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_MissingAuthMethodRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	// Absent token_endpoint_auth_method defaults to client_secret_basic per
	// RFC 7591 — a symmetric method CIMD forbids, so it must be rejected.
	doc := testDocument(testClientID)
	doc.TokenEndpointAuthMethod = ""
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_SymmetricAuthMethodRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.TokenEndpointAuthMethod = "client_secret_basic"
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_ClientSecretRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientSecret = json.RawMessage(`"s3cret"`)
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_ClientSecretExpiresAtRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientSecretExpiresAt = json.RawMessage(`0`)
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_JWKSPrivateKeyRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"EC","crv":"P-256","x":"a","y":"b","d":"private"}]}`)
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_JWKSSymmetricKeyRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"oct","k":"c2VjcmV0"}]}`)
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_JWKSPublicKeyAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"EC","crv":"P-256","x":"a","y":"b"}]}`)
	require.NoError(t, validateDocument(doc, testClientID, parsed))
}

func TestValidateDocument_OversizedClientNameRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientName = strings.Repeat("a", maxClientNameLength+1)
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata")
}

func TestValidateDocument_TooManyRedirectURIsRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = nil
	for range maxRedirectURIs + 1 {
		doc.RedirectURIs = append(doc.RedirectURIs, "https://client.example.com/callback")
	}
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri")
}

func TestValidateDocument_OversizedRedirectURIRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://client.example.com/" + strings.Repeat("a", maxRedirectURILength)}
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri")
}

func TestValidateDocument_MissingRedirectURIsRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = nil
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri")
}

func TestValidateDocument_CrossOriginHostRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://evil.example.com/callback"}
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri")
}

func TestValidateDocument_CrossOriginPortRejected(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	// No normalization: an explicit :443 is a different origin string than
	// the client_id URL's implicit port.
	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://client.example.com:443/callback"}
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri")
}

func TestValidateDocument_CustomSchemeRejectedByOriginBinding(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	// Passes the RFC 8252 scheme rules but cannot be same-origin with an
	// https client_id, so Gram's origin binding rejects it.
	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"com.example.app://callback"}
	requireOAuthError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri")
}

func TestValidateDocument_LoopbackAnyPortAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{
		"http://127.0.0.1:51423/callback",
		"http://localhost:9999/cb",
		"http://[::1]:1234/callback",
	}
	require.NoError(t, validateDocument(doc, testClientID, parsed))
}

func TestValidateDocument_UnknownExtensionFieldsAccepted(t *testing.T) {
	t.Parallel()

	raw := `{
		"client_id": "` + testClientID + `",
		"client_name": "Test Client",
		"redirect_uris": ["https://client.example.com/callback"],
		"token_endpoint_auth_method": "none",
		"software_statement": "eyJhbGciOiJub25lIn0.e30.",
		"client_id_expires_at": 4102444800,
		"x_vendor_extension": {"nested": true}
	}`
	var doc Document
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))

	parsed, err := validateClientIDURL(testClientID)
	require.NoError(t, err)
	require.NoError(t, validateDocument(&doc, testClientID, parsed))
}
