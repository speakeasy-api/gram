package remotesessions

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// tokenRequestCapture records the last token request a fake endpoint received.
type tokenRequestCapture struct {
	mu       sync.Mutex
	form     url.Values
	user     string
	password string
	basic    bool
}

func (c *tokenRequestCapture) snapshot() (url.Values, string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.form, c.user, c.password, c.basic
}

func newFakeTokenEndpoint(t *testing.T, status int, body map[string]any) (*httptest.Server, *tokenRequestCapture) {
	t.Helper()
	capture := &tokenRequestCapture{mu: sync.Mutex{}, form: nil, user: "", password: "", basic: false}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		user, password, basic := r.BasicAuth()
		capture.mu.Lock()
		capture.form, capture.user, capture.password, capture.basic = r.PostForm, user, password, basic
		capture.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func basicTokenEndpoint(srv *httptest.Server) *TokenEndpoint {
	return &TokenEndpoint{
		endpoint: srv.URL + "/token", issuer: "https://idp.example.test", issuerID: uuid.New(), doer: srv.Client(),
		auth: tokenEndpointClientAuth{Method: TokenEndpointAuthMethodBasic, RemoteSessionClientID: uuid.New(), OrganizationID: "org", JSONWebKeySetID: uuid.Nil, ClientID: "idp-client", ClientSecret: "idp-secret", AssertionAudience: "", AssertionSigner: nil},
	}
}

func TestTokenEndpointPost_AuthenticatesAsBoundClient(t *testing.T) {
	t.Parallel()
	srv, capture := newFakeTokenEndpoint(t, http.StatusOK, map[string]any{"access_token": "id-jag-value", "issued_token_type": oauthwire.TokenTypeIDJAG, "token_type": "N_A"})
	form := url.Values{}
	form.Set(oauthwire.ParamGrantType, oauthwire.GrantTypeTokenExchange)
	form.Set(oauthwire.ParamSubjectToken, "id-token-value")

	tok, err := basicTokenEndpoint(srv).Post(t.Context(), form)
	require.NoError(t, err)
	require.Equal(t, "id-jag-value", tok.AccessToken())
	require.Equal(t, oauthwire.TokenTypeIDJAG, tok.IssuedTokenType())
	require.Equal(t, "[redacted token response]", tok.String())

	sent, user, password, basic := capture.snapshot()
	require.Equal(t, oauthwire.GrantTypeTokenExchange, sent.Get("grant_type"))
	require.Equal(t, "id-token-value", sent.Get("subject_token"))
	require.True(t, basic)
	require.Equal(t, "idp-client", user)
	require.Equal(t, "idp-secret", password)
	require.Empty(t, sent.Get("client_id"), "basic clients never repeat client_id in the body")
}

func TestTokenEndpointPost_ReducesRejectionToCode(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeTokenEndpoint(t, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "secret-provider-detail"})
	_, err := basicTokenEndpoint(srv).Post(t.Context(), url.Values{})
	var failure *TokenEndpointError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, TokenEndpointError{StatusCode: http.StatusBadRequest, Code: "invalid_grant", Transport: false, Signing: false}, *failure)
	require.NotContains(t, err.Error(), "secret-provider-detail", "provider bodies never reach errors")
	require.NotContains(t, err.Error(), "idp-secret")
}

func TestTokenEndpointPost_SuccessBodyWithoutTokenIsRejection(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeTokenEndpoint(t, http.StatusOK, map[string]any{"error": "invalid_target"})
	_, err := basicTokenEndpoint(srv).Post(t.Context(), url.Values{})
	var failure *TokenEndpointError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "invalid_target", failure.Code)
}

func TestTokenEndpointPost_UnreachableIsAmbiguous(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeTokenEndpoint(t, http.StatusOK, map[string]any{})
	endpoint := basicTokenEndpoint(srv)
	srv.Close()
	_, err := endpoint.Post(t.Context(), url.Values{})
	var failure *TokenEndpointError
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.Transport)
}

func TestTokenEndpoint_FormatsRedacted(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeTokenEndpoint(t, http.StatusOK, map[string]any{})
	endpoint := basicTokenEndpoint(srv)
	require.NotContains(t, fmt.Sprintf("%v %+v %#v", endpoint, endpoint, endpoint), "idp-secret")
	encoded, err := json.Marshal(endpoint)
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(encoded))
}

func newClientEndpointManager(t *testing.T) (*ChallengeManager, *encryption.Client) {
	t.Helper()
	enc, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x24}, 32))
	require.NoError(t, err)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return &ChallengeManager{enc: enc, policy: policy}, enc
}

func resourceClientRow(t *testing.T, enc *encryption.Client, tokenEndpoint string) repo.GetRemoteSessionClientWithIssuerByIDRow {
	t.Helper()
	secret, err := enc.Encrypt([]byte("resource-secret"))
	require.NoError(t, err)
	return repo.GetRemoteSessionClientWithIssuerByIDRow{
		ClientID:                uuid.New(),
		RemoteSessionIssuerID:   uuid.New(),
		ExternalClientID:        "resource-client",
		ClientSecretEncrypted:   pgtype.Text{String: secret, Valid: true},
		TokenEndpointAuthMethod: pgtype.Text{String: string(TokenEndpointAuthMethodPost), Valid: true},
		IssuerUrl:               "https://as.resource.example.test",
		TokenEndpoint:           pgtype.Text{String: tokenEndpoint, Valid: true},
	}
}

func TestClientTokenEndpoint_AuthenticatesAsResourceClient(t *testing.T) {
	t.Parallel()
	srv, capture := newFakeTokenEndpoint(t, http.StatusOK, map[string]any{"access_token": "downstream-token", "token_type": "Bearer"})
	m, enc := newClientEndpointManager(t)
	row := resourceClientRow(t, enc, srv.URL+"/token")
	endpoint, err := m.clientTokenEndpoint(row)
	require.NoError(t, err)
	require.Equal(t, row.IssuerUrl, endpoint.Issuer())
	require.Equal(t, row.RemoteSessionIssuerID, endpoint.IssuerID())
	require.Equal(t, "resource-client", endpoint.ClientID())

	form := url.Values{}
	form.Set(oauthwire.ParamGrantType, oauthwire.GrantTypeJWTBearer)
	form.Set(oauthwire.ParamAssertion, "id-jag-value")
	_, err = endpoint.Post(t.Context(), form)
	require.NoError(t, err)
	sent, _, _, basic := capture.snapshot()
	require.False(t, basic)
	require.Equal(t, "resource-client", sent.Get("client_id"))
	require.Equal(t, "resource-secret", sent.Get("client_secret"))
	require.Equal(t, "id-jag-value", sent.Get("assertion"))
}

func TestClientTokenEndpoint_RequiresUsableRegistration(t *testing.T) {
	t.Parallel()
	m, enc := newClientEndpointManager(t)
	missingEndpoint := resourceClientRow(t, enc, "")
	missingEndpoint.TokenEndpoint = pgtype.Text{}
	_, err := m.clientTokenEndpoint(missingEndpoint)
	require.ErrorIs(t, err, ErrTokenEndpointConfiguration)

	missingSecret := resourceClientRow(t, enc, "https://as.resource.example.test/token")
	missingSecret.ClientSecretEncrypted = pgtype.Text{}
	_, err = m.clientTokenEndpoint(missingSecret)
	require.ErrorIs(t, err, ErrTokenEndpointConfiguration)
}

func TestIdentityProviderEndpointVerifyAssertion(t *testing.T) {
	t.Parallel()
	key, keys, _ := newRSAKeyPolicyFixture(t, 2048)
	endpoint := &IdentityProviderEndpoint{TokenEndpoint: &TokenEndpoint{}, keys: keys, jwksURI: rsaKeyPolicyJWKSURI, fetchScope: "example-issuer"}

	var claims jwt.Claims
	header, err := endpoint.VerifyAssertion(t.Context(), mintRSAAccessToken(t, key, jose.RS256, "oauth-id-jag+jwt"), &claims)
	require.NoError(t, err)
	require.Equal(t, "oauth-id-jag+jwt", header.ExtraHeaders[jose.HeaderType])
	require.Equal(t, rsaKeyPolicyIssuer, claims.Issuer)

	forger, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	_, err = endpoint.VerifyAssertion(t.Context(), mintRSAAccessToken(t, forger, jose.RS256, "oauth-id-jag+jwt"), &claims)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrJWTKeySetUnavailable, "a forged signature is a verdict on the token, not the key set")
}

func TestClientTokenEndpoint_PrivateKeyJWTRequiresKeySet(t *testing.T) {
	t.Parallel()
	m, enc := newClientEndpointManager(t)
	row := resourceClientRow(t, enc, "https://as.resource.example.test/token")
	row.TokenEndpointAuthMethod = pgtype.Text{String: string(TokenEndpointAuthMethodPrivateKeyJWT), Valid: true}
	row.ClientSecretEncrypted = pgtype.Text{}
	_, err := m.clientTokenEndpoint(row)
	require.ErrorIs(t, err, ErrTokenEndpointConfiguration, "a private_key_jwt client without a key set cannot sign, so it is misconfigured, not transiently failing")
}

func TestTokenEndpointErrorCode_DropsUnsafeCodes(t *testing.T) {
	t.Parallel()
	require.Equal(t, "invalid_grant", tokenEndpointErrorCode("invalid_grant"))
	require.Empty(t, tokenEndpointErrorCode("bad\ncode"), "control characters never reach logs")
	require.Empty(t, tokenEndpointErrorCode(`quoted"code`))
	require.Empty(t, tokenEndpointErrorCode(strings.Repeat("x", maxTokenEndpointErrorCodeBytes+1)), "oversized codes are dropped")
}
