package oauth21

import (
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
)

// testExternalURL is the base URL DB-backed handlers in this package are
// built with. Resource authorization server issuer identifiers derive from
// it, so mint tests can name an audience without a live listener.
const testExternalURL = "https://idp.example.com"

// dbHandler is a Handler wired to a fresh in-memory database, plus the
// handles a test needs to seed fixtures and read back what was written.
type dbHandler struct {
	*Handler
	db      *sql.DB
	queries *repo.Queries
}

// newDBHandler boots an in-memory dev-idp database, applies the schema, and
// returns a Handler bound to it. Each call is fully isolated.
func newDBHandler(t *testing.T) *dbHandler {
	t.Helper()

	logger := newTestLogger(t)
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""}, logger)
	require.NoError(t, err, "open in-memory dev-idp database")
	t.Cleanup(func() { _ = db.Close() })

	ks, err := keystore.New(nil, logger)
	require.NoError(t, err, "init keystore")

	h := NewHandler(Config{ExternalURL: testExternalURL, LoginClientID: ""}, ks, logger, newTestTracer(t), db)
	return &dbHandler{Handler: h, db: db, queries: repo.New(db)}
}

// postForm drives the handler's own mux so tests exercise routing and body
// limits the same way a real request would.
func (h *dbHandler) postForm(t *testing.T, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Handler.Handler().ServeHTTP(rec, req)
	return rec
}

// seedUser creates a user row and returns it.
func (h *dbHandler) seedUser(t *testing.T, email string) repo.User {
	t.Helper()
	user, err := h.queries.CreateUser(t.Context(), repo.CreateUserParams{
		ID:           uuid.New(),
		Email:        email,
		DisplayName:  email,
		PhotoUrl:     sql.NullString{String: "", Valid: false},
		GithubHandle: sql.NullString{String: "", Valid: false},
		Admin:        false,
		Whitelisted:  true,
	})
	require.NoError(t, err, "seed user")
	return user
}

// seedApp registers an enterprise-managed authorization requesting app.
func (h *dbHandler) seedApp(t *testing.T, clientID, secret string) repo.EmaApp {
	t.Helper()
	app, err := h.queries.CreateEmaApp(t.Context(), repo.CreateEmaAppParams{
		ID:           uuid.New(),
		ClientID:     clientID,
		ClientSecret: secret,
		Jwks:         "",
		Name:         clientID,
		Enabled:      true,
	})
	require.NoError(t, err, "seed ema app")
	return app
}

// seedResource registers a resource app. The returned row's slug determines
// the audience value a mint request must send.
func (h *dbHandler) seedResource(t *testing.T, slug, resourceIdentifier string) repo.EmaResource {
	t.Helper()
	resource, err := h.queries.CreateEmaResource(t.Context(), repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               slug,
		Name:               slug,
		ResourceIdentifier: resourceIdentifier,
	})
	require.NoError(t, err, "seed ema resource")
	return resource
}

// seedAssignment grants a user the right to drive an app against a resource.
func (h *dbHandler) seedAssignment(t *testing.T, app repo.EmaApp, user repo.User, resource repo.EmaResource, scopes string) repo.EmaAppAssignment {
	t.Helper()
	assignment, err := h.queries.CreateEmaAppAssignment(t.Context(), repo.CreateEmaAppAssignmentParams{
		ID:            uuid.New(),
		AppID:         app.ID,
		UserID:        user.ID,
		ResourceID:    resource.ID,
		GrantedScopes: scopes,
	})
	require.NoError(t, err, "seed ema assignment")
	return assignment
}

// signIDToken mints an id_token for a user exactly as the authorization_code
// grant would, so mint tests can present a real subject_token.
func (h *dbHandler) signIDToken(t *testing.T, user repo.User) string {
	t.Helper()
	token, err := h.Handler.signIDToken(t.Context(), h.queries, user.ID, "test-login-client")
	require.NoError(t, err, "sign id_token")
	return token
}

// seedRefreshToken writes an active refresh token for a user.
func (h *dbHandler) seedRefreshToken(t *testing.T, user repo.User) string {
	t.Helper()
	value := "refresh-" + uuid.NewString()
	_, err := h.queries.CreateToken(t.Context(), repo.CreateTokenParams{
		Token:     value,
		UserID:    user.ID,
		ClientID:  "test-login-client",
		Kind:      "refresh_token",
		Scope:     sql.NullString{String: "", Valid: false},
		ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err, "seed refresh token")
	return value
}

// seedAppWithJWKS registers an app that authenticates with private_key_jwt,
// returning the app row and the private key it must sign assertions with.
func (h *dbHandler) seedAppWithJWKS(t *testing.T, clientID string) (repo.EmaApp, *rsa.PrivateKey, string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "generate app key")

	const kid = "app-key-1"
	doc, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": kid,
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	})
	require.NoError(t, err, "marshal app jwks")

	app, err := h.queries.CreateEmaApp(t.Context(), repo.CreateEmaAppParams{
		ID:           uuid.New(),
		ClientID:     clientID,
		ClientSecret: "",
		Jwks:         string(doc),
		Name:         clientID,
		Enabled:      true,
	})
	require.NoError(t, err, "seed private_key_jwt app")
	return app, key, kid
}

// clientAssertionOpts describes an assertion to sign. The zero value is not
// useful; start from defaultClientAssertion.
type clientAssertionOpts struct {
	Issuer   string
	Subject  string
	Audience string
	Expires  time.Time
	Key      *rsa.PrivateKey
	KID      string
}

func (h *dbHandler) defaultClientAssertion(clientID string, key *rsa.PrivateKey, kid string) clientAssertionOpts {
	return clientAssertionOpts{
		Issuer:   clientID,
		Subject:  clientID,
		Audience: testExternalURL + Prefix,
		Expires:  time.Now().Add(2 * time.Minute),
		Key:      key,
		KID:      kid,
	}
}

func signClientAssertion(t *testing.T, opts clientAssertionOpts) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		ID:        uuid.NewString(),
		Issuer:    opts.Issuer,
		Subject:   opts.Subject,
		Audience:  jwt.ClaimStrings{opts.Audience},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(opts.Expires),
		NotBefore: nil,
	})
	token.Header["kid"] = opts.KID
	signed, err := token.SignedString(opts.Key)
	require.NoError(t, err, "sign client assertion")
	return signed
}

// cimdClient stands up a client that publishes its own metadata document,
// which is where its keys come from. `inline` chooses whether the document
// carries the JWKS directly or points at a jwks_uri; both are allowed and the
// difference is one extra fetch.
type cimdClient struct {
	clientID string
	key      *rsa.PrivateKey
	kid      string
	// fetches counts document reads, so a test can prove the resolver caches.
	fetches *atomic.Int32
}

func newCIMDClient(t *testing.T, inline bool, withKeys bool) *cimdClient {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "generate cimd client key")
	const kid = "cimd-key-1"

	var fetches atomic.Int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	clientID := server.URL + "/client.json"
	jwksJSON := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": kid,
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	}

	mux.HandleFunc("GET /client.json", func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		doc := map[string]any{
			"client_id":   clientID,
			"client_name": "CIMD Client",
		}
		switch {
		case !withKeys:
			// A document with no key material describes a public client.
		case inline:
			doc["jwks"] = jwksJSON
			doc["token_endpoint_auth_method"] = "private_key_jwt"
		default:
			doc["jwks_uri"] = server.URL + "/jwks.json"
			doc["token_endpoint_auth_method"] = "private_key_jwt"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	})

	mux.HandleFunc("GET /jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwksJSON)
	})

	return &cimdClient{clientID: clientID, key: key, kid: kid, fetches: &fetches}
}

// seedCIMDApp registers an app whose client_id is a metadata document URL and
// which therefore stores no JWKS of its own.
func (h *dbHandler) seedCIMDApp(t *testing.T, clientID string) repo.EmaApp {
	t.Helper()
	app, err := h.queries.CreateEmaApp(t.Context(), repo.CreateEmaAppParams{
		ID:           uuid.New(),
		ClientID:     clientID,
		ClientSecret: "",
		Jwks:         "",
		Name:         "CIMD Client",
		Enabled:      true,
	})
	require.NoError(t, err, "seed cimd app")
	return app
}

// newLyingCIMDClient publishes a document whose client_id names someone else,
// which the draft says a server must reject.
func newLyingCIMDClient(t *testing.T) *cimdClient {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	clientID := server.URL + "/client.json"
	mux.HandleFunc("GET /client.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id": "https://someone-else.example/client.json",
		})
	})

	var fetches atomic.Int32
	return &cimdClient{clientID: clientID, key: nil, kid: "", fetches: &fetches}
}
