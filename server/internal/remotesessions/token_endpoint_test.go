package remotesessions

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/clientsecrets"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// tokenRequestCapture records the last token request a fake endpoint received.
type tokenRequestCapture struct {
	mu        sync.Mutex
	form      url.Values
	user      string
	password  string
	basic     bool
	userAgent string
}

func (c *tokenRequestCapture) snapshot() (url.Values, string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.form, c.user, c.password, c.basic
}

func newFakeTokenEndpoint(t *testing.T, status int, body map[string]any) (*httptest.Server, *tokenRequestCapture) {
	t.Helper()
	capture := &tokenRequestCapture{mu: sync.Mutex{}, form: nil, user: "", password: "", basic: false, userAgent: ""}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		user, password, basic := r.BasicAuth()
		capture.mu.Lock()
		capture.form, capture.user, capture.password, capture.basic, capture.userAgent = r.PostForm, user, password, basic, r.UserAgent()
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
		auth: TokenEndpointClientAuth{Method: TokenEndpointAuthMethodBasic, RemoteSessionClientID: uuid.New(), OrganizationID: "org", JSONWebKeySetID: uuid.Nil, ClientID: "idp-client", ClientSecret: "idp-secret", AssertionAudience: "", AssertionSigner: nil},
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
	require.Equal(t, TokenEndpointError{StatusCode: http.StatusBadRequest, Code: "invalid_grant", Description: "secret-provider-detail", Transport: false, Signing: false}, *failure)
	require.NotContains(t, err.Error(), "secret-provider-detail", "provider bodies never reach errors")
	require.NotContains(t, err.Error(), "idp-secret")
}

func TestTokenEndpointPost_KeepsOnlySanitizedErrorDescription(t *testing.T) {
	t.Parallel()
	srv, _ := newFakeTokenEndpoint(t, http.StatusBadRequest, map[string]any{
		"error":             "invalid_grant",
		"error_description": "No seat was found\r\n\u001b[2Jfor this user." + strings.Repeat(" padding", 60),
		"assertion":         "echoed-assertion",
	})
	_, err := basicTokenEndpoint(srv).Post(t.Context(), url.Values{})
	var failure *TokenEndpointError
	require.ErrorAs(t, err, &failure)
	require.True(t, strings.HasPrefix(failure.Description, "No seat was found [2Jfor this user. padding"), failure.Description)
	require.Len(t, []rune(failure.Description), maxProviderErrorDescriptionRunes+1)
	require.NotContains(t, failure.Description, "echoed-assertion")

	vendor, _ := newFakeTokenEndpoint(t, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "invalid_grant", "message": "vendor-detail"}})
	_, err = basicTokenEndpoint(vendor).Post(t.Context(), url.Values{})
	require.ErrorAs(t, err, &failure)
	require.Empty(t, failure.Description, "only the RFC 6749 error_description member is kept")
}

func TestProviderErrorDescription(t *testing.T) {
	t.Parallel()
	require.Equal(t, "line one line two", ProviderErrorDescription("line one\r\n\tline two"))
	require.Equal(t, "beep", ProviderErrorDescription("\x1b\x07be\u200bep\x00"))
	require.Empty(t, ProviderErrorDescription(" \n "))
	long := ProviderErrorDescription(strings.Repeat("é", 500))
	require.Equal(t, strings.Repeat("é", maxProviderErrorDescriptionRunes)+"…", long)
}

func TestTokenEndpointPost_SendsSpeakeasyUserAgent(t *testing.T) {
	t.Parallel()
	srv, capture := newFakeTokenEndpoint(t, http.StatusOK, map[string]any{"access_token": "at", "token_type": "Bearer"})
	_, err := basicTokenEndpoint(srv).Post(t.Context(), url.Values{})
	require.NoError(t, err)
	capture.mu.Lock()
	defer capture.mu.Unlock()
	require.Equal(t, constants.UserAgent, capture.userAgent)
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
	return &ChallengeManager{enc: enc, secrets: clientsecrets.WithoutOrganizationEncryption(testenv.NewLogger(t), nil, enc), policy: policy}, enc
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
	endpoint, err := m.clientTokenEndpoint(t.Context(), row)
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
	_, err := m.clientTokenEndpoint(t.Context(), missingEndpoint)
	require.ErrorIs(t, err, ErrTokenEndpointConfiguration)

	missingSecret := resourceClientRow(t, enc, "https://as.resource.example.test/token")
	missingSecret.ClientSecretEncrypted = pgtype.Text{}
	_, err = m.clientTokenEndpoint(t.Context(), missingSecret)
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
	_, err := m.clientTokenEndpoint(t.Context(), row)
	require.ErrorIs(t, err, ErrTokenEndpointConfiguration, "a private_key_jwt client without a key set cannot sign, so it is misconfigured, not transiently failing")
}

func TestTokenEndpointErrorCode_DropsUnsafeCodes(t *testing.T) {
	t.Parallel()
	require.Equal(t, "invalid_grant", tokenEndpointErrorCode("invalid_grant"))
	require.Empty(t, tokenEndpointErrorCode("bad\ncode"), "control characters never reach logs")
	require.Empty(t, tokenEndpointErrorCode(`quoted"code`))
	require.Empty(t, tokenEndpointErrorCode(strings.Repeat("x", maxTokenEndpointErrorCodeBytes+1)), "oversized codes are dropped")
}

type failingAssertionSigner struct{ err error }

func (s failingAssertionSigner) SignClientAssertion(context.Context, ClientAssertionRequest) (string, error) {
	return "", s.err
}

func TestTokenEndpointPost_SigningFailures(t *testing.T) {
	t.Parallel()
	post := func(t *testing.T, signErr error) error {
		t.Helper()
		endpoint := &TokenEndpoint{endpoint: "https://as.resource.example.test/token", auth: TokenEndpointClientAuth{Method: TokenEndpointAuthMethodPrivateKeyJWT, AssertionSigner: failingAssertionSigner{signErr}}}
		_, err := endpoint.Post(t.Context(), url.Values{})
		return err
	}
	for name, cause := range map[string]error{
		"no key set":         fmt.Errorf("no key set: %w", errClientAssertionKeyUnconfigured),
		"active key deleted": fmt.Errorf("read active key: %w", pgx.ErrNoRows),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, post(t, cause), ErrTokenEndpointConfiguration, "this fails every attempt, so it isn't worth retrying")
		})
	}
	t.Run("signing outage stays temporary", func(t *testing.T) {
		t.Parallel()
		failure, ok := errors.AsType[*TokenEndpointError](post(t, errors.New("kms unavailable")))
		require.True(t, ok)
		require.True(t, failure.Signing)
	})
}

func TestClientTokenEndpoint_KeyOutageIsNotMisconfiguration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	require.NotNil(t, DelegationTestDatabase, "TestMain must provide the shared database clone factory")
	db, err := DelegationTestDatabase(t, "client_secret_key_outage")
	require.NoError(t, err)
	organizationID := "org_" + uuid.NewString()
	require.NoError(t, testrepo.New(db).SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: organizationID, Name: "Key outage organization", Slug: "key-outage-" + uuid.NewString()[:8]}))
	// The organization's client secrets are past platform_key, and this
	// process has no organization encryption to open them with.
	_, err = orgencryptionrepo.New(db).CreateEncryptionTransition(ctx, orgencryptionrepo.CreateEncryptionTransitionParams{
		OrganizationID:          organizationID,
		UseCase:                 string(orgencryption.UseCaseOAuthClientSecrets),
		State:                   string(orgencryption.TransitionStateDualWrite),
		PlatformCopiesClearedAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)

	m, enc := newClientEndpointManager(t)
	m.secrets = clientsecrets.WithoutOrganizationEncryption(testenv.NewLogger(t), db, enc)
	row := resourceClientRow(t, enc, "https://as.resource.example.test/token")
	row.ClientOrganizationID = pgtype.Text{String: organizationID, Valid: true}

	_, err = m.clientTokenEndpoint(ctx, row)
	require.ErrorIs(t, err, orgencryption.ErrOrganizationEncryptionUnavailable)
	require.NotErrorIs(t, err, ErrTokenEndpointConfiguration, "a key the process cannot use is retryable, not a broken client configuration")
}

// orgKeyClient is a client whose organization is in org_kms, so its secret is
// stored only as the organization-key copy, read back with the row a reader
// loads.
type orgKeyClient struct {
	fixture        *orgencryptiontest.Fixture
	secrets        *clientsecrets.Secrets
	organizationID string
	row            repo.GetRemoteSessionClientWithIssuerByIDRow
}

func newOrgKeyClient(t *testing.T, tokenEndpoint string) orgKeyClient {
	t.Helper()
	ctx := t.Context()
	require.NotNil(t, DelegationTestDatabase, "TestMain must provide the shared database clone factory")
	db, err := DelegationTestDatabase(t, "client_secret_org_key_reader")
	require.NoError(t, err)
	organizationID := "org_" + uuid.NewString()
	require.NoError(t, testrepo.New(db).SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: organizationID, Name: "Organization key reader", Slug: "org-key-reader-" + uuid.NewString()[:8]}))

	fixture := orgencryptiontest.New(t, db)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), db, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	_, err = transitions.StartInOrgKMS(ctx, organizationID, orgencryption.UseCaseOAuthClientSecrets, orgencryption.SystemActor())
	require.NoError(t, err)
	enc, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x24}, 32))
	require.NoError(t, err)
	secrets := clientsecrets.New(testenv.NewLogger(t), orgencryption.NewCipher(testenv.NewLogger(t), enc, fixture.Service, transitions), enc)

	q := repo.New(db)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		OrganizationID:                      pgtype.Text{String: organizationID, Valid: true},
		Slug:                                "org-key-reader-" + uuid.NewString()[:8],
		Issuer:                              "https://as.resource.example.test",
		TokenEndpoint:                       pgtype.Text{String: tokenEndpoint, Valid: true},
		ScopesSupported:                     []string{},
		GrantTypesSupported:                 []string{},
		AuthorizationGrantProfilesSupported: []string{},
		ResponseTypesSupported:              []string{},
		TokenEndpointAuthMethodsSupported:   []string{},
		CodeChallengeMethodsSupported:       []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{},
		IDTokenSigningAlgValuesSupported:          []string{},
		ClaimsSupported:                           []string{},
	})
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		OrganizationID:          pgtype.Text{String: organizationID, Valid: true},
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "resource-client",
		TokenEndpointAuthMethod: pgtype.Text{String: string(TokenEndpointAuthMethodPost), Valid: true},
	})
	require.NoError(t, err)
	stored, err := secrets.Set(ctx, db, client.ID, clientOwner(client), []byte("resource-secret"), pgtype.Timestamptz{})
	require.NoError(t, err)
	require.False(t, stored.ClientSecretEncrypted.Valid, "org_kms stores only the organization-key copy")

	row, err := q.GetRemoteSessionClientWithIssuerByID(ctx, client.ID)
	require.NoError(t, err)
	return orgKeyClient{fixture: fixture, secrets: secrets, organizationID: organizationID, row: row}
}

func TestClientTokenEndpoint_OpensTheOrganizationKeyCopy(t *testing.T) {
	t.Parallel()
	srv, capture := newFakeTokenEndpoint(t, http.StatusOK, map[string]any{"access_token": "downstream-token", "token_type": "Bearer"})
	client := newOrgKeyClient(t, srv.URL+"/token")
	m, _ := newClientEndpointManager(t)
	m.secrets = client.secrets

	endpoint, err := m.clientTokenEndpoint(t.Context(), client.row)
	require.NoError(t, err)
	form := url.Values{}
	form.Set(oauthwire.ParamGrantType, oauthwire.GrantTypeJWTBearer)
	form.Set(oauthwire.ParamAssertion, "id-jag-value")
	_, err = endpoint.Post(t.Context(), form)
	require.NoError(t, err)
	sent, _, _, _ := capture.snapshot()
	require.Equal(t, "resource-secret", sent.Get("client_secret"))

	// The owner comes from the client row: without its organization the
	// organization-key copy cannot be opened, which is what a wrong owner
	// would look like after finalize.
	unowned := client.row
	unowned.ClientOrganizationID = pgtype.Text{}
	_, err = m.clientTokenEndpoint(t.Context(), unowned)
	require.ErrorIs(t, err, ErrTokenEndpointConfiguration)
}

func TestClientTokenEndpoint_SuspendedOrganizationKeyIsRetryable(t *testing.T) {
	t.Parallel()
	client := newOrgKeyClient(t, "https://as.resource.example.test/token")
	m, _ := newClientEndpointManager(t)
	m.secrets = client.secrets
	require.NoError(t, client.fixture.Provisioner.Suspend(t.Context(), client.organizationID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := m.clientTokenEndpoint(t.Context(), client.row)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrTokenEndpointConfiguration, "a suspended key is retryable, not a broken client configuration")
	require.True(t, clientsecrets.IsKeyProblem(err))
}

func TestEnrichmentTarget_OpensTheOrganizationKeyCopy(t *testing.T) {
	t.Parallel()
	client := newOrgKeyClient(t, "https://as.resource.example.test/token")

	secret, err := enrichmentTargetFromClient(client.row, client.organizationID, client.secrets).clientSecret(t.Context())
	require.NoError(t, err)
	require.Equal(t, "resource-secret", secret)

}
