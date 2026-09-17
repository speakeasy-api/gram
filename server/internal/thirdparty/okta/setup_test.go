package okta

import (
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/dpop"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

const (
	stubClientID   = "0oa-stub-client"
	stubScopes     = "okta.apps.read okta.groups.read"
	proofIATLeeway = 60 * time.Second
)

var stubAssertionSecret = []byte("stub-assertion-secret-0123456789abcdef")

// fakeClock is shared by the client and the stub so iat checks agree.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// stubSigner mints HS256 client assertions the stub server can verify.
type stubSigner struct {
	clock    *fakeClock
	mu       sync.Mutex
	calls    int
	requests []remotesessions.ClientAssertionRequest
	jtis     []string
	replay   bool
	last     string

	// assertions holds every assertion returned, including replays.
	assertions []string
}

func (s *stubSigner) SignClientAssertion(_ context.Context, req remotesessions.ClientAssertionRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.requests = append(s.requests, req)
	if s.replay && s.last != "" {
		s.assertions = append(s.assertions, s.last)
		return s.last, nil
	}

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: stubAssertionSecret}, nil)
	if err != nil {
		return "", fmt.Errorf("stub signer: %w", err)
	}
	jti := uuid.NewString()
	now := s.clock.Now()
	assertion, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer:    req.ClientID,
		Subject:   req.ClientID,
		Audience:  jwt.Audience{req.Audience},
		Expiry:    jwt.NewNumericDate(now.Add(time.Minute)),
		NotBefore: nil,
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        jti,
	}).Serialize()
	if err != nil {
		return "", fmt.Errorf("stub signer serialize: %w", err)
	}
	s.jtis = append(s.jtis, jti)
	s.last = assertion
	s.assertions = append(s.assertions, assertion)
	return assertion, nil
}

func (s *stubSigner) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubSigner) JTIs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.jtis)
}

func (s *stubSigner) Last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

func (s *stubSigner) Assertions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.assertions)
}

func (s *stubSigner) setReplay(replay bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replay = replay
}

// proofRecord is one verified DPoP proof seen by the stub.
type proofRecord struct {
	method string
	htm    string
	htu    string
	ath    string
	nonce  string
	jti    string
}

// stubCounts is a locked snapshot of the stub's request counters.
type stubCounts struct {
	tokenRequests  int
	assertionsSeen int
	issuedTokens   int
}

// stubOkta is an httptest Okta org with a DPoP token endpoint and DPoP-bound resources.
type stubOkta struct {
	t     *testing.T
	srv   *httptest.Server
	clock *fakeClock

	mu                   sync.Mutex
	requireTokenNonce    bool
	rotateTokenNonce     bool
	tokenNonce           string
	lastAppsQuery        url.Values
	groupsQueries        []url.Values
	emitResourceNonce    string
	requireResourceNonce bool
	resourceNonce        string
	grantedScopes        []string
	scopeErrorCode       string

	tokenRateLimitLimit     int
	tokenRateLimitRemaining int
	tokenRateLimitReset     int64
	proofJTIs               map[string]bool
	assertionJTIs           map[string]bool
	tokens                  map[string]string
	tokenCount              int
	tokenRequests           int
	assertionsSeen          int
	proofs                  []proofRecord
	apps                    []appJSON
	appUsers                map[string][]appUserJSON
	appGroups               map[string][]appGroupJSON
	groups                  []groupJSON
	pageSize                int
	pending429              int
	rateLimitLimit          int
	rateLimitRemaining      int
	rateLimitReset          int64
	tokenType               string
	tokenRedirect           string
	tokenPending429         int
	overrides               map[string]http.HandlerFunc
}

func newStubOkta(t *testing.T, clock *fakeClock) *stubOkta {
	t.Helper()
	s := &stubOkta{
		t:                       t,
		srv:                     nil,
		clock:                   clock,
		mu:                      sync.Mutex{},
		requireTokenNonce:       true,
		rotateTokenNonce:        false,
		tokenNonce:              "nonce-1",
		lastAppsQuery:           nil,
		emitResourceNonce:       "",
		requireResourceNonce:    false,
		resourceNonce:           "",
		grantedScopes:           strings.Fields(stubScopes),
		scopeErrorCode:          "invalid_scope",
		tokenRateLimitLimit:     0,
		tokenRateLimitRemaining: 0,
		tokenRateLimitReset:     0,
		proofJTIs:               map[string]bool{},
		assertionJTIs:           map[string]bool{},
		tokens:                  map[string]string{},
		tokenCount:              0,
		tokenRequests:           0,
		assertionsSeen:          0,
		proofs:                  nil,
		apps:                    nil,
		appUsers:                map[string][]appUserJSON{},
		appGroups:               map[string][]appGroupJSON{},
		groups:                  nil,
		pageSize:                0,
		pending429:              0,
		rateLimitLimit:          0,
		rateLimitRemaining:      0,
		rateLimitReset:          0,
		tokenType:               "DPoP",
		tokenRedirect:           "",
		tokenPending429:         0,
		overrides:               map[string]http.HandlerFunc{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/v1/token", s.handleToken)
	mux.HandleFunc("GET /api/v1/apps", s.resource(s.handleListApps))
	mux.HandleFunc("GET /api/v1/apps/{id}", s.resource(s.handleGetApp))
	mux.HandleFunc("GET /api/v1/apps/{id}/users", s.resource(s.handleAppUsers))
	mux.HandleFunc("GET /api/v1/apps/{id}/groups", s.resource(s.handleAppGroups))
	mux.HandleFunc("GET /api/v1/groups", s.resource(s.handleListGroups))
	mux.HandleFunc("GET /", s.resource(s.handleOverride))
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubOkta) setApps(apps []appJSON) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apps = apps
}

func (s *stubOkta) setAppUsers(appID string, users []appUserJSON) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appUsers[appID] = users
}

func (s *stubOkta) setAppGroups(appID string, groups []appGroupJSON) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appGroups[appID] = groups
}

func (s *stubOkta) setGroups(groups []groupJSON) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups = groups
}

func (s *stubOkta) setPageSize(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageSize = n
}

func (s *stubOkta) setPending429(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending429 = n
}

func (s *stubOkta) setRateLimit(limit, remaining int, reset int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rateLimitLimit = limit
	s.rateLimitRemaining = remaining
	s.rateLimitReset = reset
}

// setTokenRateLimit reports quota headers on every token response, not only 429s.
func (s *stubOkta) setTokenRateLimit(limit, remaining int, reset int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenRateLimitLimit, s.tokenRateLimitRemaining, s.tokenRateLimitReset = limit, remaining, reset
}

func (s *stubOkta) setTokenType(tokenType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenType = tokenType
}

// setTokenRedirect makes the token endpoint answer 307 to location.
func (s *stubOkta) setTokenRedirect(location string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenRedirect = location
}

// setTokenPending429 makes the next n token requests answer 429 with the
// configured rate limit headers.
func (s *stubOkta) setTokenPending429(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenPending429 = n
}

func (s *stubOkta) setTokenNonce(nonce string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenNonce = nonce
}

func (s *stubOkta) setRotateTokenNonce(rotate bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rotateTokenNonce = rotate
}

func (s *stubOkta) appsQuery() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAppsQuery
}

func (s *stubOkta) setResourceNonce(require bool, nonce string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requireResourceNonce = require
	s.resourceNonce = nonce
}

func (s *stubOkta) setEmitResourceNonce(nonce string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitResourceNonce = nonce
}

// setScopeErrorCode picks the OAuth error for an all-ungranted scope request;
// Okta returns invalid_scope or consent_required depending on the org.
func (s *stubOkta) setScopeErrorCode(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scopeErrorCode = code
}

func (s *stubOkta) setGrantedScopes(scopes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grantedScopes = scopes
}

func (s *stubOkta) setOverride(path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[path] = h
}

func (s *stubOkta) counts() stubCounts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return stubCounts{tokenRequests: s.tokenRequests, assertionsSeen: s.assertionsSeen, issuedTokens: len(s.tokens)}
}

func (s *stubOkta) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.t.Errorf("encode stub response: %v", err)
	}
}

func (s *stubOkta) rejectProof(w http.ResponseWriter, status int, description string) (proofRecord, string, bool) {
	s.writeJSON(w, status, map[string]string{"error": "invalid_dpop_proof", "error_description": description})
	return proofRecord{}, "", false
}

// verifyProof checks a DPoP proof and records it; false means the error was written.
func (s *stubOkta) verifyProof(w http.ResponseWriter, r *http.Request, accessToken string) (proofRecord, string, bool) {
	raw := r.Header.Get("DPoP")
	if raw == "" {
		return s.rejectProof(w, http.StatusBadRequest, "missing proof")
	}
	parsed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return s.rejectProof(w, http.StatusBadRequest, err.Error())
	}
	header := parsed.Signatures[0].Header
	if typ, _ := header.ExtraHeaders[jose.HeaderType].(string); typ != dpop.ProofType {
		return s.rejectProof(w, http.StatusBadRequest, "wrong typ")
	}
	if header.JSONWebKey == nil {
		return s.rejectProof(w, http.StatusBadRequest, "missing jwk")
	}
	payload, err := parsed.Verify(header.JSONWebKey)
	if err != nil {
		return s.rejectProof(w, http.StatusBadRequest, "bad signature")
	}
	var claims struct {
		JTI   string `json:"jti"`
		HTM   string `json:"htm"`
		HTU   string `json:"htu"`
		ATH   string `json:"ath"`
		Nonce string `json:"nonce"`
		IAT   int64  `json:"iat"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return s.rejectProof(w, http.StatusBadRequest, "bad claims")
	}
	if skew := s.clock.Now().Sub(time.Unix(claims.IAT, 0)); skew > proofIATLeeway || skew < -proofIATLeeway {
		return s.rejectProof(w, http.StatusBadRequest, "iat out of range")
	}
	thumb, err := header.JSONWebKey.Thumbprint(crypto.SHA256)
	if err != nil {
		return s.rejectProof(w, http.StatusBadRequest, "bad jwk")
	}
	thumbprint := base64.RawURLEncoding.EncodeToString(thumb)

	// EscapedPath keeps the wire form so escaped ids compare against the signed htu.
	expectedHTU := s.srv.URL + r.URL.EscapedPath()
	if claims.HTM != r.Method || claims.HTU != expectedHTU {
		return s.rejectProof(w, http.StatusBadRequest, "htm/htu mismatch")
	}
	if accessToken == "" {
		if claims.ATH != "" {
			return s.rejectProof(w, http.StatusBadRequest, "unexpected ath")
		}
	} else {
		sum := sha256.Sum256([]byte(accessToken))
		if claims.ATH != base64.RawURLEncoding.EncodeToString(sum[:]) {
			return s.rejectProof(w, http.StatusUnauthorized, "ath mismatch")
		}
	}

	s.mu.Lock()
	replay := s.proofJTIs[claims.JTI]
	s.proofJTIs[claims.JTI] = true
	s.mu.Unlock()
	if replay || claims.JTI == "" {
		return s.rejectProof(w, http.StatusBadRequest, "jti replayed")
	}

	rec := proofRecord{method: r.Method, htm: claims.HTM, htu: claims.HTU, ath: claims.ATH, nonce: claims.Nonce, jti: claims.JTI}
	s.mu.Lock()
	s.proofs = append(s.proofs, rec)
	s.mu.Unlock()
	return rec, thumbprint, true
}

func (s *stubOkta) handleToken(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.tokenRequests++
	redirect := s.tokenRedirect
	throttle := s.tokenPending429 > 0
	if throttle {
		s.tokenPending429--
	}
	if throttle && s.rateLimitLimit > 0 {
		w.Header().Set("X-Rate-Limit-Limit", strconv.Itoa(s.rateLimitLimit))
		w.Header().Set("X-Rate-Limit-Remaining", strconv.Itoa(s.rateLimitRemaining))
		w.Header().Set("X-Rate-Limit-Reset", strconv.FormatInt(s.rateLimitReset, 10))
	}
	if s.tokenRateLimitLimit > 0 {
		w.Header().Set("X-Rate-Limit-Limit", strconv.Itoa(s.tokenRateLimitLimit))
		w.Header().Set("X-Rate-Limit-Remaining", strconv.Itoa(s.tokenRateLimitRemaining))
		w.Header().Set("X-Rate-Limit-Reset", strconv.FormatInt(s.tokenRateLimitReset, 10))
	}
	s.mu.Unlock()
	if redirect != "" {
		http.Redirect(w, r, redirect, http.StatusTemporaryRedirect)
		return
	}
	if throttle {
		s.writeJSON(w, http.StatusTooManyRequests, map[string]string{"errorCode": "E0000047", "errorSummary": "API call exceeded rate limit due to too many requests."})
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": err.Error()})
		return
	}

	proof, thumbprint, ok := s.verifyProof(w, r, "")
	if !ok {
		return
	}

	// Like Okta, the assertion jti burns before the nonce check.
	assertion := r.Form.Get("client_assertion")
	if assertion == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client", "error_description": "missing client assertion"})
		return
	}
	if r.Form.Get("client_assertion_type") != oauthwire.ClientAssertionTypeJWTBearer {
		s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": "bad assertion type"})
		return
	}
	if r.Form.Get("client_id") != stubClientID {
		s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": "client_id mismatch"})
		return
	}
	s.mu.Lock()
	s.assertionsSeen++
	s.mu.Unlock()

	tok, err := jwt.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.HS256})
	if err != nil {
		s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": "malformed assertion"})
		return
	}
	var claims jwt.Claims
	if err := tok.Claims(stubAssertionSecret, &claims); err != nil {
		s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": "bad assertion signature"})
		return
	}
	if claims.Issuer != stubClientID || claims.Subject != stubClientID || !claims.Audience.Contains(s.srv.URL+tokenEndpointPath) {
		s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": "assertion claims mismatch"})
		return
	}
	if claims.ID == "" {
		s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": "client assertion missing jti"})
		return
	}
	s.mu.Lock()
	replay := s.assertionJTIs[claims.ID]
	s.assertionJTIs[claims.ID] = true
	s.mu.Unlock()
	if replay {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_client", "error_description": "The client_assertion token has already been used"})
		return
	}

	s.mu.Lock()
	requireNonce, nonce, granted, tokenType, scopeErr := s.requireTokenNonce, s.tokenNonce, s.grantedScopes, s.tokenType, s.scopeErrorCode
	if s.rotateTokenNonce {
		s.tokenNonce = "rotated-" + uuid.NewString()
	}
	s.mu.Unlock()
	if requireNonce && proof.nonce != nonce {
		w.Header().Set("DPoP-Nonce", nonce)
		s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use_dpop_nonce", "error_description": "Authorization server requires nonce in DPoP proof."})
		return
	}

	// Ungranted scopes are trimmed; an all-ungranted request fails with scopeErrorCode.
	scope := granted
	if requested := strings.Fields(r.Form.Get("scope")); len(requested) > 0 {
		scope = make([]string, 0, len(requested))
		for _, sc := range requested {
			if slices.Contains(granted, sc) {
				scope = append(scope, sc)
			}
		}
		if len(scope) == 0 {
			s.writeJSON(w, http.StatusBadRequest, map[string]string{"error": scopeErr, "error_description": "One or more scopes are not configured for the authorization server resource."})
			return
		}
	}

	s.mu.Lock()
	s.tokenCount++
	token := fmt.Sprintf("stub-access-token-%d-%s", s.tokenCount, uuid.NewString())
	s.tokens[token] = thumbprint
	s.mu.Unlock()

	s.writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   tokenType,
		"expires_in":   3600,
		"scope":        strings.Join(scope, " "),
	})
}

func (s *stubOkta) resource(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		s.mu.Lock()
		thumb, known := s.tokens[token]
		s.mu.Unlock()
		if scheme != "DPoP" || !known {
			w.Header().Set("WWW-Authenticate", `DPoP error="invalid_token"`)
			s.writeJSON(w, http.StatusUnauthorized, map[string]string{"errorCode": "E0000011", "errorSummary": "Invalid token provided"})
			return
		}
		proof, proofThumb, ok := s.verifyProof(w, r, token)
		if !ok {
			return
		}
		if proofThumb != thumb {
			w.Header().Set("WWW-Authenticate", `DPoP error="invalid_token"`)
			s.writeJSON(w, http.StatusUnauthorized, map[string]string{"errorCode": "E0000011", "errorSummary": "Token not bound to proof key"})
			return
		}

		s.mu.Lock()
		requireNonce, nonce := s.requireResourceNonce, s.resourceNonce
		s.mu.Unlock()
		if requireNonce && proof.nonce != nonce {
			w.Header().Set("DPoP-Nonce", nonce)
			w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
			s.writeJSON(w, http.StatusUnauthorized, map[string]string{"errorCode": "E0000011", "errorSummary": "nonce required"})
			return
		}

		s.mu.Lock()
		if s.emitResourceNonce != "" {
			w.Header().Set("DPoP-Nonce", s.emitResourceNonce)
		}
		if s.rateLimitLimit > 0 {
			w.Header().Set("X-Rate-Limit-Limit", strconv.Itoa(s.rateLimitLimit))
			w.Header().Set("X-Rate-Limit-Remaining", strconv.Itoa(s.rateLimitRemaining))
			w.Header().Set("X-Rate-Limit-Reset", strconv.FormatInt(s.rateLimitReset, 10))
		}
		throttle := s.pending429 > 0
		if throttle {
			s.pending429--
		}
		s.mu.Unlock()
		if throttle {
			s.writeJSON(w, http.StatusTooManyRequests, map[string]string{"errorCode": "E0000047", "errorSummary": "API call exceeded rate limit due to too many requests."})
			return
		}
		next(w, r)
	}
}

func (s *stubOkta) handleOverride(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	h, ok := s.overrides[r.URL.Path]
	s.mu.Unlock()
	if !ok {
		s.writeJSON(w, http.StatusNotFound, map[string]string{"errorCode": "E0000007", "errorSummary": "Not found"})
		return
	}
	h(w, r)
}

// paginate slices items by pageSize using an integer "after" cursor.
func paginate[T any](s *stubOkta, w http.ResponseWriter, r *http.Request, items []T) []T {
	s.mu.Lock()
	size := s.pageSize
	s.mu.Unlock()
	if size <= 0 || len(items) <= size {
		return items
	}
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	end := min(after+size, len(items))
	if end < len(items) {
		next := *r.URL
		next.Scheme, next.Host = "http", r.Host
		q := next.Query()
		q.Set("after", strconv.Itoa(end))
		next.RawQuery = q.Encode()
		w.Header().Add("Link", fmt.Sprintf(`<%s>; rel="self"`, r.URL.String()))
		w.Header().Add("Link", fmt.Sprintf(`<%s>; rel="next"`, next.String()))
	}
	return items[after:end]
}

func (s *stubOkta) handleListApps(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	apps := s.apps
	s.lastAppsQuery = r.URL.Query()
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, paginate(s, w, r, apps))
}

func (s *stubOkta) handleGetApp(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, app := range s.apps {
		if app.ID == r.PathValue("id") {
			s.writeJSON(w, http.StatusOK, app)
			return
		}
	}
	s.writeJSON(w, http.StatusNotFound, map[string]string{"errorCode": "E0000007", "errorSummary": "Not found: Resource not found"})
}

func (s *stubOkta) handleAppUsers(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	users := s.appUsers[r.PathValue("id")]
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, paginate(s, w, r, users))
}

func (s *stubOkta) handleAppGroups(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	groups := s.appGroups[r.PathValue("id")]
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, paginate(s, w, r, groups))
}

func (s *stubOkta) handleListGroups(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	groups := s.groups
	s.groupsQueries = append(s.groupsQueries, r.URL.Query())
	s.mu.Unlock()
	if r.URL.Query().Has("q") {
		http.Error(w, "q does not support pagination", http.StatusBadRequest)
		return
	}
	if search := r.URL.Query().Get("search"); search != "" {
		var prefix string
		if !strings.HasPrefix(search, "profile.name sw ") || json.Unmarshal([]byte(strings.TrimPrefix(search, "profile.name sw ")), &prefix) != nil {
			http.Error(w, "invalid search expression", http.StatusBadRequest)
			return
		}
		filtered := make([]groupJSON, 0)
		for _, group := range groups {
			if strings.HasPrefix(strings.ToLower(group.Profile.Name), strings.ToLower(prefix)) {
				filtered = append(filtered, group)
			}
		}
		groups = filtered
	}
	s.writeJSON(w, http.StatusOK, paginate(s, w, r, groups))
}

func (s *stubOkta) recordedProofs() []proofRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.proofs)
}

func (s *stubOkta) issuedTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.tokens))
	for tok := range s.tokens {
		out = append(out, tok)
	}
	return out
}

// fakeSleeper records requested sleeps instead of blocking.
type fakeSleeper struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (f *fakeSleeper) sleep(_ context.Context, d time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waits = append(f.waits, d)
	return nil
}

func (f *fakeSleeper) Waits() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.waits)
}

type testClient struct {
	client  *httpClient
	stub    *stubOkta
	signer  *stubSigner
	sleeper *fakeSleeper
	clock   *fakeClock
}

func (tc testClient) currentNonce() string {
	return tc.client.nonce.Current()
}

func (tc testClient) clearSlowdown() {
	tc.client.mu.Lock()
	defer tc.client.mu.Unlock()
	tc.client.slowUntil = time.Time{}
}

func newTestClient(t *testing.T, tracerProvider trace.TracerProvider, logger *slog.Logger, cfg Config) testClient {
	t.Helper()
	clock := &fakeClock{mu: sync.Mutex{}, now: time.Now().Truncate(time.Second)}
	stub := newStubOkta(t, clock)
	signer := &stubSigner{clock: clock, mu: sync.Mutex{}, calls: 0, requests: nil, jtis: nil, replay: false, last: ""}
	sleeper := &fakeSleeper{mu: sync.Mutex{}, waits: nil}

	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	factory, ok := NewClientFactory(logger, policy, signer).(*clientFactory)
	require.True(t, ok)

	cfg.OrgURL = stub.srv.URL
	cfg.ClientID = stubClientID
	cfg.AudienceFormat = string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint)
	if cfg.RemoteSessionClientID == uuid.Nil {
		cfg.RemoteSessionClientID = uuid.New()
	}
	if cfg.OrganizationID == "" {
		cfg.OrganizationID = "org_test"
	}
	if cfg.JSONWebKeySetID == uuid.Nil {
		cfg.JSONWebKeySetID = uuid.New()
	}

	client, err := factory.Client(cfg)
	require.NoError(t, err)
	impl, ok := client.(*httpClient)
	require.True(t, ok)
	impl.sleep = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("test sleep: %w", err)
		}
		clock.advance(d)
		return sleeper.sleep(ctx, d)
	}
	impl.now = clock.Now
	impl.jitter = func() time.Duration { return maxRateLimitJitter / 2 }

	return testClient{client: impl, stub: stub, signer: signer, sleeper: sleeper, clock: clock}
}

func testConfig() Config {
	return Config{
		OrgURL:                "",
		ClientID:              "",
		AudienceFormat:        "",
		RemoteSessionClientID: uuid.Nil,
		OrganizationID:        "",
		JSONWebKeySetID:       uuid.Nil,
		MaxPages:              0,
	}
}

func newDefaultTestClient(t *testing.T) testClient {
	t.Helper()
	return newTestClient(t, testenv.NewTracerProvider(t), testenv.NewLogger(t), testConfig())
}

func stubApps(n int) []appJSON {
	apps := make([]appJSON, 0, n)
	for i := range n {
		apps = append(apps, appJSON{
			ID:          fmt.Sprintf("0oa%03d", i),
			Label:       fmt.Sprintf("App %d", i),
			Name:        "oidc_client",
			SignOnMode:  "OPENID_CONNECT",
			Status:      "ACTIVE",
			Features:    []string{"PUSH_NEW_USERS"},
			Created:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			LastUpdated: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		})
	}
	return apps
}

// sinkServer records every request that reaches it; the client must never
// send one here.
type sinkServer struct {
	srv *httptest.Server

	mu   sync.Mutex
	hits []*http.Request
}

func newSinkServer(t *testing.T) *sinkServer {
	t.Helper()
	sink := &sinkServer{srv: nil, mu: sync.Mutex{}, hits: nil}
	sink.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sink.mu.Lock()
		sink.hits = append(sink.hits, r)
		sink.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"leaked","token_type":"DPoP","expires_in":3600}`))
	}))
	t.Cleanup(sink.srv.Close)
	return sink
}

func (s *sinkServer) Hits() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.hits)
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
