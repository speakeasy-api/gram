package resourceas

import (
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
	"github.com/speakeasy-api/gram/dev-idp/internal/xaa"
	"github.com/speakeasy-api/gram/plog"
)

const (
	testResourceSlug = "chat"
	testResourceID   = "https://mcp.chat.example/"
	testClientID     = "requesting-app"
)

// harness is a resource authorization server wired to a fresh in-memory
// database, mounted on a real listener so discovery documents and outbound
// JWKS fetches behave as they would in the binary.
type harness struct {
	*Handler
	queries  *repo.Queries
	server   *httptest.Server
	baseURL  string
	keystore *keystore.Keystore
	resource repo.XaaResource
	user     repo.User
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	logger := plog.NewLogger(io.Discard)
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""}, logger)
	require.NoError(t, err, "open in-memory dev-idp database")
	t.Cleanup(func() { _ = db.Close() })

	ks, err := keystore.New(nil, logger)
	require.NoError(t, err, "init keystore")

	outer := http.NewServeMux()
	server := httptest.NewUnstartedServer(outer)
	baseURL := "http://" + server.Listener.Addr().String()

	h := NewHandler(Config{ExternalURL: baseURL}, ks, logger, tracenoop.NewTracerProvider(), db)
	outer.Handle(Prefix+"/", http.StripPrefix(Prefix, h.Handler()))
	h.RegisterRootRoutes(outer)

	server.Start()
	t.Cleanup(server.Close)

	queries := repo.New(db)
	resource, err := queries.CreateXaaResource(t.Context(), repo.CreateXaaResourceParams{
		ID:                 uuid.New(),
		Slug:               testResourceSlug,
		Name:               "Chat",
		ResourceIdentifier: testResourceID,
	})
	require.NoError(t, err, "seed resource")

	user, err := queries.CreateUser(t.Context(), repo.CreateUserParams{
		ID:           uuid.New(),
		Email:        "redeem@devidptest.local",
		DisplayName:  "Redeem",
		PhotoUrl:     sql.NullString{String: "", Valid: false},
		GithubHandle: sql.NullString{String: "", Valid: false},
		Admin:        false,
		Whitelisted:  true,
	})
	require.NoError(t, err, "seed user")

	return &harness{
		Handler:  h,
		queries:  queries,
		server:   server,
		baseURL:  baseURL,
		keystore: ks,
		resource: resource,
		user:     user,
	}
}

// localIssuer is the oauth2-1 issuer identifier this dev-idp would mint under.
func (h *harness) localIssuer() string { return h.baseURL + "/oauth2-1" }

// audience is the resource authorization server's own issuer identifier.
func (h *harness) audience() string { return xaa.ResourceASIssuer(h.baseURL, testResourceSlug) }

// trustLocalIssuer adds a trust rule accepting this dev-idp's own IdP.
func (h *harness) trustLocalIssuer(t *testing.T, allowedScopes string) repo.XaaTrustRule {
	t.Helper()
	return h.trustIssuer(t, h.localIssuer(), allowedScopes, "[]")
}

func (h *harness) trustIssuer(t *testing.T, issuer, allowedScopes, allowedClientIDs string) repo.XaaTrustRule {
	t.Helper()
	rule, err := h.queries.CreateXaaTrustRule(t.Context(), repo.CreateXaaTrustRuleParams{
		ID:               uuid.New(),
		ResourceID:       h.resource.ID,
		TrustedIssuer:    issuer,
		AllowedClientIds: allowedClientIDs,
		AllowedScopes:    allowedScopes,
		Enabled:          true,
	})
	require.NoError(t, err, "seed trust rule")
	return rule
}

// jagOpts describes an ID-JAG to mint for a test. The zero value is not
// useful; use h.signJAG which fills in the conformant defaults.
type jagOpts struct {
	Issuer   string
	Subject  string
	Email    string
	Audience string
	Resource string
	ClientID string
	Scope    string
	JTI      string
	IssuedAt time.Time
	Expires  time.Time
	Typ      string
	Key      *rsa.PrivateKey
	KID      string
}

// defaultJAG returns options that produce an ID-JAG this harness accepts.
// Tests change exactly one field to express the case under test.
func (h *harness) defaultJAG() jagOpts {
	now := time.Now()
	return jagOpts{
		Issuer:   h.localIssuer(),
		Subject:  h.user.ID.String(),
		Email:    h.user.Email,
		Audience: h.audience(),
		Resource: testResourceID,
		ClientID: testClientID,
		Scope:    "chat.read chat.history",
		JTI:      uuid.NewString(),
		IssuedAt: now,
		Expires:  now.Add(5 * time.Minute),
		Typ:      xaa.JWTType,
		Key:      nil, // nil means "sign with this dev-idp's own key"
		KID:      "",
	}
}

func (h *harness) signJAG(t *testing.T, opts jagOpts) string {
	t.Helper()

	claims := xaa.Claims{
		Email:    opts.Email,
		Resource: opts.Resource,
		ClientID: opts.ClientID,
		Scope:    opts.Scope,
		AuthTime: opts.IssuedAt.Unix(),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        opts.JTI,
			Issuer:    opts.Issuer,
			Subject:   opts.Subject,
			Audience:  jwt.ClaimStrings{opts.Audience},
			IssuedAt:  jwt.NewNumericDate(opts.IssuedAt),
			ExpiresAt: jwt.NewNumericDate(opts.Expires),
			NotBefore: nil,
		},
	}

	key := opts.Key
	kid := opts.KID
	if key == nil {
		key = h.keystore.PrivateKey()
		kid = h.keystore.KID()
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	token.Header["typ"] = opts.Typ
	signed, err := token.SignedString(key)
	require.NoError(t, err, "sign test ID-JAG")
	return signed
}

// response is a fully-read HTTP response. Helpers return this rather than an
// *http.Response so the body is closed at the point it is read, which keeps
// every test free of cleanup bookkeeping.
type response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// redeem posts an assertion to the resource's token endpoint.
func (h *harness) redeem(t *testing.T, assertion, clientID string) response {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", xaa.GrantTypeJWTBearer)
	form.Set("assertion", assertion)
	form.Set("client_id", clientID)
	return h.postForm(t, "/token", form)
}

func (h *harness) postForm(t *testing.T, path string, form url.Values) response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.audience()+path, strings.NewReader(form.Encode()))
	require.NoError(t, err, "build request")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return h.do(t, req)
}

// get issues a GET against an absolute URL on this harness's listener.
func (h *harness) get(t *testing.T, rawURL string) response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	require.NoError(t, err, "build request")
	return h.do(t, req)
}

func (h *harness) do(t *testing.T, req *http.Request) response {
	t.Helper()

	resp, err := h.server.Client().Do(req)
	require.NoError(t, err, "%s %s", req.Method, req.URL)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read response body")

	return response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}
}

func decodeJSON[T any](t *testing.T, resp response) T {
	t.Helper()
	var out T
	require.NoError(t, json.Unmarshal(resp.Body, &out), "decode response body: %s", resp.Body)
	return out
}

// foreignIDP stands up a second, independent issuer: its own RSA key, its own
// RFC 8414 metadata document and its own JWKS. It exists so trust rules can
// be tested against an issuer this dev-idp has no private knowledge of, which
// is the only way the discovery-and-JWKS path is really exercised.
type foreignIDP struct {
	issuer string
	key    *rsa.PrivateKey
	kid    string
}

func newForeignIDP(t *testing.T) *foreignIDP {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "generate foreign idp key")
	const kid = "foreign-key-1"

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	issuer := server.URL + "/idp"

	mux.HandleFunc("GET /.well-known/oauth-authorization-server/idp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":   issuer,
			"jwks_uri": issuer + "/jwks.json",
		})
	})

	mux.HandleFunc("GET /idp/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})

	return &foreignIDP{issuer: issuer, key: key, kid: kid}
}

// nullString is the "leave this column alone" value for the partial-update
// queries, whose COALESCE keeps the existing value when the parameter is NULL.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
