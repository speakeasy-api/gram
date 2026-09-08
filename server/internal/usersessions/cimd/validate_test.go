package cimd

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// testDocument returns a document that passes every validateDocument check
// for the given client_id URL: https plus loopback redirect URIs, public
// auth method, required client_name.
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
	findings, err := validateDocument(testDocument(testClientID), testClientID, parsed)
	require.NoError(t, err)
	require.Empty(t, findings.crossOriginRedirectOrigins)
}

func TestValidateDocument_ClientIDMismatchRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientID = "https://client.example.com/oauth/other.json"
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonClientIDMismatch)
}

func TestValidateDocument_MissingClientNameRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientName = ""
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonMissingClientName)
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
	_, err = validateDocument(doc, testClientID, parsed)
	require.NoError(t, err)
}

// testPublicJWKS is a single-key public JWK Set holding a real, freshly
// generated ES256 key: the validators refuse a set with no usable signing
// key, and a hand-written coordinate pair is not one.
func testPublicJWKS(t *testing.T) json.RawMessage {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: key.Public(), KeyID: "k1", Algorithm: string(jose.ES256), Use: "sig"}}}
	body, err := json.Marshal(set)
	require.NoError(t, err)
	return body
}

// A private_key_jwt document is accepted with exactly one key source, inline
// or remote: that is what the token endpoint will verify its assertions
// against.
func TestValidateDocument_PrivateKeyJWTAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	inline := testDocument(testClientID)
	inline.TokenEndpointAuthMethod = "private_key_jwt"
	inline.JWKS = testPublicJWKS(t)
	_, err = validateDocument(inline, testClientID, parsed)
	require.NoError(t, err)

	remote := testDocument(testClientID)
	remote.TokenEndpointAuthMethod = "private_key_jwt"
	remote.JWKSURI = "https://keys.client.example.com/jwks.json"
	_, err = validateDocument(remote, testClientID, parsed)
	require.NoError(t, err, "a jwks_uri on a different host than the client_id is fine: no origin rule applies")
}

// Declaring private_key_jwt without a key source leaves nothing to verify
// against, and is refused as its own reason rather than as a bad method.
func TestValidateDocument_PrivateKeyJWTRequiresKeySource(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.TokenEndpointAuthMethod = "private_key_jwt"
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonKeySourceMissing)
}

// RFC 7591 §2 forbids naming both key sources, whatever the method: it leaves
// the authoritative set undefined.
func TestValidateDocument_BothKeySourcesRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = testPublicJWKS(t)
	doc.JWKSURI = "https://keys.client.example.com/jwks.json"
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonKeySourceAmbiguous)
}

// A jwks_uri is syntax-checked with the same rule the resolver applies, so a
// document accepted here cannot fail to resolve later for a reason that was
// visible up front. It is never fetched at validation time.
func TestValidateDocument_JWKSURIInvalidRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	for _, uri := range []string{"http://keys.client.example.com/jwks.json", "https://user@keys.client.example.com/jwks.json", "not a url"} {
		doc := testDocument(testClientID)
		doc.TokenEndpointAuthMethod = "private_key_jwt"
		doc.JWKSURI = uri
		_, err = validateDocument(doc, testClientID, parsed)
		requireValidationError(t, err, "invalid_client_metadata", reasonJWKSURIInvalid)
	}
}

// A jwks that is present but holds no key set does not count as a key
// source. An explicit null is treated as absent, so a private_key_jwt
// document carrying only a null jwks is refused for the missing source it
// is, and null alongside a jwks_uri is not "both present"; a set with no
// keys array is refused as malformed, since resolve-time parsing would refuse
// it anyway and the client could never authenticate.
func TestValidateDocument_DegenerateJWKS(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	nullOnly := testDocument(testClientID)
	nullOnly.TokenEndpointAuthMethod = "private_key_jwt"
	nullOnly.JWKS = json.RawMessage(`null`)
	_, err = validateDocument(nullOnly, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonKeySourceMissing)

	nullWithURI := testDocument(testClientID)
	nullWithURI.TokenEndpointAuthMethod = "private_key_jwt"
	nullWithURI.JWKS = json.RawMessage(`null`)
	nullWithURI.JWKSURI = "https://keys.client.example.com/jwks.json"
	_, err = validateDocument(nullWithURI, testClientID, parsed)
	require.NoError(t, err, "a null jwks next to a jwks_uri is one key source, not two")

	for _, raw := range []string{`{}`, `{"keys":null}`} {
		noKeys := testDocument(testClientID)
		noKeys.TokenEndpointAuthMethod = "private_key_jwt"
		noKeys.JWKS = json.RawMessage(raw)
		_, err = validateDocument(noKeys, testClientID, parsed)
		requireValidationError(t, err, "invalid_client_metadata", reasonJWKSInvalid)
	}

	// A well-formed set the resolver would parse to nothing usable.
	for _, raw := range []string{`{"keys":[]}`, `{"keys":[{"kty":"EC","crv":"P-256","kid":"k1","x":"AA","y":"AA","use":"enc"}]}`} {
		unusable := testDocument(testClientID)
		unusable.TokenEndpointAuthMethod = "private_key_jwt"
		unusable.JWKS = json.RawMessage(raw)
		_, err = validateDocument(unusable, testClientID, parsed)
		requireValidationError(t, err, "invalid_client_metadata", reasonJWKSNoSigningKey)
	}
}

// A public client may still publish keys; the source rules apply to it but
// nothing requires one.
func TestValidateDocument_PublicClientMayPublishKeys(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKSURI = "https://keys.client.example.com/jwks.json"
	_, err = validateDocument(doc, testClientID, parsed)
	require.NoError(t, err)
}

func TestValidateDocument_SymmetricAuthMethodRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.TokenEndpointAuthMethod = "client_secret_basic"
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonInvalidAuthMethod)
}

func TestValidateDocument_ClientSecretRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientSecret = json.RawMessage(`"s3cret"`)
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonContainsSecret)
}

func TestValidateDocument_ClientSecretExpiresAtRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientSecretExpiresAt = json.RawMessage(`0`)
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonContainsSecret)
}

func TestValidateDocument_JWKSNotAJWKSetRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`[]`)
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonJWKSInvalid)
}

func TestValidateDocument_JWKSPrivateKeyRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"EC","crv":"P-256","x":"a","y":"b","d":"private"}]}`)
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonJWKSPrivateKey)
}

func TestValidateDocument_JWKSSymmetricKeyRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"oct","k":"c2VjcmV0"}]}`)
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonJWKSSymmetricKey)
}

func TestValidateDocument_JWKSPublicKeyAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.JWKS = json.RawMessage(`{"keys":[{"kty":"EC","crv":"P-256","x":"a","y":"b"}]}`)
	_, err = validateDocument(doc, testClientID, parsed)
	require.NoError(t, err)
}

func TestValidateDocument_OversizedClientNameRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.ClientName = strings.Repeat("a", maxClientNameLength+1)
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_client_metadata", reasonClientNameTooLong)
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
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_redirect_uri", reasonTooManyRedirectURIs)
}

func TestValidateDocument_OversizedRedirectURIRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://client.example.com/" + strings.Repeat("a", maxRedirectURILength)}
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_redirect_uri", reasonRedirectURITooLong)
}

func TestValidateDocument_MissingRedirectURIsRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = nil
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_redirect_uri", reasonMissingRedirectURIs)
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
	// RFC 8252 §7.3 rule before the CIMD-specific https scheme rule runs.
	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"http://client.example.com/callback"}
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_redirect_uri", reasonRedirectURIInvalid)
}

// TestValidateDocument_CrossOriginHostAccepted pins the removal of the
// redirect-URI same-origin binding (AIS-597): https redirect_uris on a
// different host than the client_id URL are valid, and their distinct
// origins come back as findings in first-seen order for the observability
// emission. Same-origin entries produce no finding.
func TestValidateDocument_CrossOriginHostAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{
		"https://api.other.example/callback",
		"https://api.other.example/second",
		"https://client.example.com/callback",
	}
	findings, err := validateDocument(doc, testClientID, parsed)
	require.NoError(t, err)
	require.Equal(t, []string{"https://api.other.example"}, findings.crossOriginRedirectOrigins)
}

func TestValidateDocument_CrossOriginPortAccepted(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	// No normalization: an explicit :443 is a different origin string than
	// the client_id URL's implicit port, so the entry is accepted but
	// reported as cross-origin.
	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://client.example.com:443/callback"}
	findings, err := validateDocument(doc, testClientID, parsed)
	require.NoError(t, err)
	require.Equal(t, []string{"https://client.example.com:443"}, findings.crossOriginRedirectOrigins)
}

// TestValidateDocument_HTTPSLoopbackCountedCrossOrigin pins the boundary of
// the loopback exemption: IsLoopbackRedirectURI exempts only http loopback
// URLs (RFC 8252 §7.3), so an https URL on a loopback host passes the scheme
// rule as a regular https redirect and counts as cross-origin.
func TestValidateDocument_HTTPSLoopbackCountedCrossOrigin(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"https://localhost:8080/callback"}
	findings, err := validateDocument(doc, testClientID, parsed)
	require.NoError(t, err)
	require.Equal(t, []string{"https://localhost:8080"}, findings.crossOriginRedirectOrigins)
}

// TestValidateDocument_CustomSchemeRedirectRejected pins the CIMD-specific
// scheme rule: oauthwire.ValidateRedirectURI tolerates native-app custom
// schemes (kept for DCR clients, AIS-434), but a document's non-loopback
// redirect_uris must use https.
func TestValidateDocument_CustomSchemeRedirectRejected(t *testing.T) {
	t.Parallel()

	parsed, err := ValidateClientIDURL(testClientID)
	require.NoError(t, err)

	doc := testDocument(testClientID)
	doc.RedirectURIs = []string{"com.example.app://callback"}
	_, err = validateDocument(doc, testClientID, parsed)
	requireValidationError(t, err, "invalid_redirect_uri", reasonRedirectURIScheme)
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
	findings, err := validateDocument(doc, testClientID, parsed)
	require.NoError(t, err)
	require.Empty(t, findings.crossOriginRedirectOrigins, "http loopback redirects are exempt from cross-origin reporting")
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
	_, err = validateDocument(&doc, testClientID, parsed)
	require.NoError(t, err)
}
