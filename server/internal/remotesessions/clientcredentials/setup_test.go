package clientcredentials

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// tokenResponse is a token endpoint reply: an HTTP status and a JSON body.
type tokenResponse struct {
	status int
	body   map[string]any
}

// bearerToken is a successful client credentials response.
func bearerToken(accessToken string) tokenResponse {
	return tokenResponse{status: http.StatusOK, body: map[string]any{"access_token": accessToken, "token_type": "Bearer", "expires_in": 3600}}
}

// oauthError is an RFC 6749 §5.2 error response.
func oauthError(status int, code string) tokenResponse {
	return tokenResponse{status: status, body: map[string]any{"error": code}}
}

// tokenRequest is one request the fake token endpoint received.
type tokenRequest struct {
	form   url.Values
	header http.Header
}

// tokenServer is a fake token endpoint that records requests and answers with
// respond, called with the zero-based request number.
type tokenServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []tokenRequest
	respond  func(n int) tokenResponse
}

func newTokenServer(t *testing.T, respond func(n int) tokenResponse) *tokenServer {
	t.Helper()

	ts := &tokenServer{server: nil, mu: sync.Mutex{}, requests: nil, respond: respond}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/token" {
			http.Error(w, "not the token endpoint", http.StatusNotFound)

			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		ts.mu.Lock()
		n := len(ts.requests)
		ts.requests = append(ts.requests, tokenRequest{form: r.PostForm, header: r.Header.Clone()})
		ts.mu.Unlock()

		reply := ts.respond(n)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.status)
		_ = json.NewEncoder(w).Encode(reply.body)
	}))
	t.Cleanup(ts.server.Close)

	return ts
}

func (ts *tokenServer) tokenURL() string { return ts.server.URL + "/token" }

func (ts *tokenServer) received() []tokenRequest {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	return append([]tokenRequest(nil), ts.requests...)
}

// recordingSigner is a client assertion signer that records each request and
// returns an assertion naming the requested audience.
type recordingSigner struct {
	mu       sync.Mutex
	requests []remotesessions.ClientAssertionRequest
}

func (s *recordingSigner) SignClientAssertion(_ context.Context, req remotesessions.ClientAssertionRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests = append(s.requests, req)

	return "assertion-for-" + req.Audience, nil
}

func (s *recordingSigner) received() []remotesessions.ClientAssertionRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]remotesessions.ClientAssertionRequest(nil), s.requests...)
}

// fixture is one organization with an issuer whose token endpoint is a fake.
type fixture struct {
	db       *pgxpool.Pool
	enc      *encryption.Client
	signer   *recordingSigner
	tokens   *tokenServer
	org      string
	issuerID uuid.UUID
}

// issuerOptions are the issuer fields a test varies.
type issuerOptions struct {
	resourceIndicatorSupported pgtype.Bool
}

func newFixture(t *testing.T, respond func(n int) tokenResponse) fixture {
	t.Helper()

	return newFixtureWithIssuer(t, respond, issuerOptions{resourceIndicatorSupported: pgtype.Bool{}})
}

func newFixtureWithIssuer(t *testing.T, respond func(n int) tokenResponse, opts issuerOptions) fixture {
	t.Helper()

	ctx := t.Context()

	db, err := infra.CloneTestDatabase(t, "client_credentials")
	require.NoError(t, err)

	enc, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x42}, 32))
	require.NoError(t, err)

	org := "org-" + uuid.NewString()
	require.NoError(t, testrepo.New(db).SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: org, Name: "Client credentials organization", Slug: "client-credentials-" + uuid.NewString()[:8]}))

	tokens := newTokenServer(t, respond)
	f := fixture{db: db, enc: enc, signer: &recordingSigner{mu: sync.Mutex{}, requests: nil}, tokens: tokens, org: org, issuerID: uuid.Nil}

	params := f.issuerParams(pgtype.Text{String: org, Valid: true})
	params.ResourceIndicatorSupported = opts.resourceIndicatorSupported

	issuer, err := repo.New(db).CreateRemoteSessionIssuer(ctx, params)
	require.NoError(t, err)

	f.issuerID = issuer.ID

	return f
}

// issuerParams describes an issuer at the fake token endpoint, owned by
// organizationID or global when it is NULL.
func (f fixture) issuerParams(organizationID pgtype.Text) repo.CreateRemoteSessionIssuerParams {
	return repo.CreateRemoteSessionIssuerParams{
		OrganizationID: organizationID, Slug: "upstream-" + uuid.NewString()[:8], Issuer: f.tokens.server.URL,
		TokenEndpoint:                     pgtype.Text{String: f.tokens.tokenURL(), Valid: true},
		GrantTypesSupported:               []string{oauthwire.GrantTypeClientCredentials},
		TokenEndpointAuthMethodsSupported: []string{oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost, oauthwire.AuthMethodPrivateKeyJWT},
		ScopesSupported:                   []string{}, ResponseTypesSupported: []string{}, CodeChallengeMethodsSupported: []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{}, IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
	}
}

// secretClientAt registers an organization client_secret_basic client at
// issuerID.
func (f fixture) secretClientAt(t *testing.T, issuerID uuid.UUID) uuid.UUID {
	t.Helper()

	encrypted, err := f.enc.Encrypt([]byte("client-secret"))
	require.NoError(t, err)

	client, err := repo.New(f.db).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		OrganizationID: pgtype.Text{String: f.org, Valid: true}, RemoteSessionIssuerID: issuerID, ClientID: "gram-client",
		ClientSecretEncrypted:   pgtype.Text{String: encrypted, Valid: true},
		TokenEndpointAuthMethod: pgtype.Text{String: oauthwire.AuthMethodClientSecretBasic, Valid: true},
		Scope:                   []string{},
	})
	require.NoError(t, err)

	return client.ID
}

// newMinter builds a minter with its own Redis connection and challenge
// manager, as a separate replica would have, over the fixture's database.
func (f fixture) newMinter(t *testing.T) *Minter {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	store := cache.NewRedisCacheAdapter(redisClient)

	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)

	challenges := remotesessions.NewChallengeManager(logger, tracerProvider, testenv.NewMeterProvider(t), f.db, f.enc, policy, nil, store, serverURL, remotesessions.WithTokenEndpointAssertionSigner(f.signer))

	return New(logger, f.db, f.enc, challenges, store)
}

// secretClient registers an organization client authenticating with a secret.
func (f fixture) secretClient(t *testing.T, method string, scope []string) uuid.UUID {
	t.Helper()

	encrypted, err := f.enc.Encrypt([]byte("client-secret"))
	require.NoError(t, err)

	client, err := repo.New(f.db).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		OrganizationID: pgtype.Text{String: f.org, Valid: true}, RemoteSessionIssuerID: f.issuerID, ClientID: "gram-client",
		ClientSecretEncrypted:   pgtype.Text{String: encrypted, Valid: true},
		TokenEndpointAuthMethod: pgtype.Text{String: method, Valid: true},
		Scope:                   scope,
	})
	require.NoError(t, err)

	return client.ID
}

// keySetClient registers an organization private_key_jwt client over a key set
// with an active key, and returns the client and the key set.
func (f fixture) keySetClient(t *testing.T, audienceFormat string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	ctx := t.Context()

	keySetID, err := testrepo.New(f.db).SeedJsonWebKeySetFixture(ctx, testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: f.org, Name: "signing-" + uuid.NewString()[:8]})
	require.NoError(t, err)

	f.activateKey(t, keySetID, "kid-1")

	client, err := repo.New(f.db).CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		OrganizationID: pgtype.Text{String: f.org, Valid: true}, RemoteSessionIssuerID: f.issuerID, ClientID: "gram-client",
		TokenEndpointAuthMethod:         pgtype.Text{String: oauthwire.AuthMethodPrivateKeyJWT, Valid: true},
		TokenEndpointAuthAudienceFormat: pgtype.Text{String: audienceFormat, Valid: audienceFormat != ""},
		JsonWebKeySetID:                 uuid.NullUUID{UUID: keySetID, Valid: true},
		Scope:                           []string{},
	})
	require.NoError(t, err)

	return client.ID, keySetID
}

// activateKey retires the key set's active key, if any, and activates a new
// one with kid.
func (f fixture) activateKey(t *testing.T, keySetID uuid.UUID, kid string) {
	t.Helper()

	ctx := t.Context()
	keys := jwksrepo.New(f.db)

	set, err := keys.GetJsonWebKeySet(ctx, jwksrepo.GetJsonWebKeySetParams{ID: keySetID, OrganizationID: f.org})
	require.NoError(t, err)

	// A set without an active key has nothing to retire.
	if _, err := keys.RetireActiveJsonWebKey(ctx, jwksrepo.RetireActiveJsonWebKeyParams{JsonWebKeySetID: keySetID, OrganizationID: f.org, ExcludeID: uuid.Nil}); err != nil {
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}

	_, err = keys.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: f.org, JsonWebKeySetID: keySetID, ExternalKeyID: set.ExternalKeyID, State: "active", Kid: kid, PublicJwk: []byte(`{}`)})
	require.NoError(t, err)
}

// rotateSecret replaces the client's secret.
func (f fixture) rotateSecret(t *testing.T, clientID uuid.UUID, secret string) {
	t.Helper()

	encrypted, err := f.enc.Encrypt([]byte(secret))
	require.NoError(t, err)

	require.NoError(t, repo.New(f.db).SetOrganizationRemoteSessionClientCredentialsFixture(t.Context(), repo.SetOrganizationRemoteSessionClientCredentialsFixtureParams{
		ClientID: pgtype.Text{}, ClientSecretEncrypted: pgtype.Text{String: encrypted, Valid: true},
		ID: clientID, OrganizationID: pgtype.Text{String: f.org, Valid: true},
	}))
}

// cacheKeys are the keys the minter uses for the client and resource.
func (f fixture) cacheKeys(t *testing.T, clientID uuid.UUID, resource string) cacheKeys {
	t.Helper()

	client, err := repo.New(f.db).GetClientCredentialsGrantClient(t.Context(), repo.GetClientCredentialsGrantClientParams{ID: clientID, OrganizationID: pgtype.Text{String: f.org, Valid: true}})
	require.NoError(t, err)

	return newCacheKeys(client, sentResource(client, resource))
}

func (f fixture) request(clientID uuid.UUID, resource string) Request {
	return Request{OrganizationID: f.org, ClientID: clientID, Resource: resource}
}
