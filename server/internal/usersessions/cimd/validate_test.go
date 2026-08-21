package cimd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
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
	var oauthErr *oauthwire.Error
	require.ErrorAs(t, err, &oauthErr)
	require.Equal(t, code, oauthErr.Code)
}

// requireValidationError asserts both halves of a validation rejection: the
// client-facing OAuth error code reachable through errors.As, and the metric
// reason label extracted by validationReasonOf.
func requireValidationError(t *testing.T, err error, code string, reason validationReason) {
	t.Helper()
	requireOAuthError(t, err, code)
	require.Equal(t, reason, validationReasonOf(err))
}

func TestValidateClientIDURL_Valid(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)
	require.Equal(t, "client.example.com", parsed.Host)
	require.Equal(t, "/oauth/client.json", parsed.Path)
}

func TestValidateClientIDURL_QueryTolerated(t *testing.T) {
	t.Parallel()

	_, err := ValidateClientIDURL("https://client.example.com/oauth/client.json?v=2")
	require.NoError(t, err)
}

func TestValidateClientIDURL_MissingPathRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateClientIDURL("https://client.example.com")
	requireValidationError(t, err, "invalid_request", reasonClientIDMissingPath)
}

func TestValidateClientIDURL_UserinfoRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateClientIDURL("https://user@client.example.com/client.json")
	requireValidationError(t, err, "invalid_request", reasonClientIDUserinfo)
}

func TestValidateClientIDURL_FragmentRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateClientIDURL("https://client.example.com/client.json#frag")
	requireValidationError(t, err, "invalid_request", reasonClientIDFragment)
}

func TestValidateClientIDURL_DotDotSegmentRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateClientIDURL("https://client.example.com/oauth/../client.json")
	requireValidationError(t, err, "invalid_request", reasonClientIDDotSegments)
}

func TestValidateClientIDURL_DotSegmentRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateClientIDURL("https://client.example.com/./client.json")
	requireValidationError(t, err, "invalid_request", reasonClientIDDotSegments)
}

func TestValidateClientIDURL_NonHTTPSRejected(t *testing.T) {
	t.Parallel()

	_, err := ValidateClientIDURL("http://client.example.com/client.json")
	requireValidationError(t, err, "invalid_request", reasonClientIDScheme)
}

func TestValidateClientIDURL_OversizedRejected(t *testing.T) {
	t.Parallel()

	long := "https://client.example.com/" + strings.Repeat("a", maxClientIDLength)
	_, err := ValidateClientIDURL(long)
	requireValidationError(t, err, "invalid_request", reasonClientIDTooLong)
}

func TestValidateDocument_Valid(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)
	require.NoError(t, validateDocument(testDocument(testClientID), testClientID, parsed))
}

func TestValidateDocument_ClientIDMismatchRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientID = "https://client.example.com/oauth/other.json"
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonClientIDMismatch)
}

func TestValidateDocument_MissingClientNameRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientName = ""
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonMissingClientName)
}

// TestValidateDocument_MissingAuthMethodAccepted: an absent
// token_endpoint_auth_method means "none", not client_secret_basic.
//
// RFC 7591's default does not carry over, because -02 §4.1 forbids a CIMD
// document from using any shared-symmetric-secret method — so absence
// cannot denote one. The field is not in -02's required set either.
//
// This is not academic. OpenAI's documents (ChatGPT and Codex CLI) omit the
// field while being ordinary public clients, so rejecting here would make
// the ChatGPT presets admit and then fail validation one step later — the
// worst outcome available, since the client cannot fall back.
func TestValidateDocument_MissingAuthMethodAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.TokenEndpointAuthMethod = ""
	require.NoError(t, validateDocument(doc, testClientID, parsed))
}

// TestValidateDocument_AsymmetricAuthMethodRejected pins that dropping the
// absent-value rejection did NOT open the door to confidential clients. An
// explicit private_key_jwt is still refused until §8.2's RFC 7523 §2.2
// enforcement lands with it.
func TestValidateDocument_AsymmetricAuthMethodRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.TokenEndpointAuthMethod = "private_key_jwt"
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonInvalidAuthMethod)
}

func TestValidateDocument_SymmetricAuthMethodRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.TokenEndpointAuthMethod = "client_secret_basic"
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonInvalidAuthMethod)
}

func TestValidateDocument_ClientSecretRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientSecret = json.RawMessage(`"s3cret"`)
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonContainsSecret)
}

func TestValidateDocument_ClientSecretExpiresAtRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientSecretExpiresAt = json.RawMessage(`0`)
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonContainsSecret)
}

func TestValidateDocument_JWKSNotAJWKSetRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`[]`)
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonJWKSInvalid)
}

func TestValidateDocument_JWKSPrivateKeyRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"EC","crv":"P-256","x":"a","y":"b","d":"private"}]}`)
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonJWKSPrivateKey)
}

func TestValidateDocument_JWKSSymmetricKeyRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"oct","k":"c2VjcmV0"}]}`)
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonJWKSSymmetricKey)
}

func TestValidateDocument_JWKSPublicKeyAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"EC","crv":"P-256","x":"a","y":"b"}]}`)
	require.NoError(t, validateDocument(doc, testClientID, parsed))
}

func TestValidateDocument_OversizedClientNameRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientName = strings.Repeat("a", maxClientNameLength+1)
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_client_metadata", reasonClientNameTooLong)
}

func TestValidateDocument_TooManyRedirectURIsRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = nil
	for range maxRedirectURIs + 1 {
		doc.RedirectURIs = append(doc.RedirectURIs, "https://client.example.com/callback")
	}
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri", reasonTooManyRedirectURIs)
}

func TestValidateDocument_OversizedRedirectURIRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://client.example.com/" + strings.Repeat("a", maxRedirectURILength)}
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri", reasonRedirectURITooLong)
}

func TestValidateDocument_MissingRedirectURIsRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = nil
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri", reasonMissingRedirectURIs)
}

// TestValidateDocument_NonLoopbackHTTPRedirectURIRejected exercises the
// oauthwire.ValidateRedirectURI rejection path — the one site whose
// wrapping shape differs (a fmt.Errorf layer between the validationError and
// the OAuthError), so both the reason extraction and the errors.As traversal
// through the extra layer are pinned here.
func TestValidateDocument_NonLoopbackHTTPRedirectURIRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	// An http redirect on a non-loopback host fails ValidateRedirectURI's
	// RFC 8252 §7.3 rule before origin binding is consulted.
	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"http://client.example.com/callback"}
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri", reasonRedirectURIInvalid)
}

func TestValidateDocument_CrossOriginHostRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://evil.example.com/callback"}
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri", reasonRedirectOriginMismatch)
}

func TestValidateDocument_CrossOriginPortRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	// No normalization: an explicit :443 is a different origin string than
	// the client_id URL's implicit port.
	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://client.example.com:443/callback"}
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri", reasonRedirectOriginMismatch)
}

func TestValidateDocument_CustomSchemeRejectedByOriginBinding(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	// Passes the RFC 8252 scheme rules but cannot be same-origin with an
	// https client_id, so Gram's origin binding rejects it.
	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"com.example.app://callback"}
	requireValidationError(t, validateDocument(doc, testClientID, parsed), "invalid_redirect_uri", reasonRedirectOriginMismatch)
}

func TestValidateDocument_LoopbackAnyPortAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
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

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)
	require.NoError(t, validateDocument(&doc, testClientID, parsed))
}
