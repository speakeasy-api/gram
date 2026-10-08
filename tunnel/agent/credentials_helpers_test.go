package agent

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/identity"
	"github.com/speakeasy-api/gram/tunnel/jwks"
)

const (
	testIssuer   = "https://tunnel.example.test"
	testAudience = "tunneled-mcp-server:00000000-0000-4000-8000-000000000001"
	testOrg      = "org_test"
)

// Synthetic tokens. They are compared only by digest.
const (
	testTokenA = "synthetic-token-a"
	testTokenB = "synthetic-token-b"
)

var (
	testKeyOnce sync.Once
	testKey     *rsa.PrivateKey
	testKeyPEM  string
)

// sharedTestKey is generated once: RSA generation dominates test time.
func sharedTestKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	testKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			panic(err)
		}
		testKey = key
		testKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	})
	return testKey, testKeyPEM
}

// testSigner mints assertions and serves its JWKS.
type testSigner struct {
	key  *rsa.PrivateKey
	kid  string
	srv  *httptest.Server
	hits atomic.Int64
	// down makes the JWKS endpoint fail.
	down atomic.Bool
}

func newTestSigner(t *testing.T) *testSigner {
	t.Helper()
	key, publicPEM := sharedTestKey(t)
	set, err := jwks.Parse(publicPEM)
	require.NoError(t, err)
	pub, err := jwks.PublicKey(&key.PublicKey)
	require.NoError(t, err)
	s := &testSigner{key: key, kid: pub.KeyID}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if s.down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		set.ServeHTTP(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *testSigner) config() CredentialsConfig {
	return CredentialsConfig{
		Issuer:         testIssuer,
		Audience:       testAudience,
		OrganizationID: testOrg,
		JWKSURL:        s.srv.URL + jwks.Path,
		AllowInsecure:  true,
		Root:           "",
		MaxAge:         time.Hour,
	}
}

type testClock struct{ now atomic.Int64 }

func newTestClock() *testClock {
	c := &testClock{}
	c.now.Store(time.Now().UnixNano())
	return c
}

func (c *testClock) Now() time.Time { return time.Unix(0, c.now.Load()) }

func (c *testClock) Advance(d time.Duration) { c.now.Add(int64(d)) }

type testPrincipal struct {
	subject     string
	mcpServerID string
	consent     bool
}

var defaultTestPrincipal = testPrincipal{subject: "user:alice", mcpServerID: "00000000-0000-4000-8000-0000000000aa", consent: false}

type testGrant struct {
	clientID   string
	grantID    string
	generation int64
}

var defaultTestGrant = testGrant{clientID: "00000000-0000-4000-8000-0000000000c1", grantID: "00000000-0000-4000-8000-0000000000d1", generation: 1}

// credentialClaim builds the upstream_credential claim for token.
func credentialClaim(g testGrant, token string, expiresAt time.Time) map[string]any {
	claim := map[string]any{
		"owner":            identity.OwnerSubject,
		"client_id":        g.clientID,
		"grant_id":         g.grantID,
		"grant_generation": g.generation,
		"token_sha256":     identity.TokenSHA256(token),
	}
	if !expiresAt.IsZero() {
		claim["token_expires_at"] = expiresAt.Unix()
	}
	return claim
}

// claims returns valid assertion claims; edit the map to make them invalid.
func (s *testSigner) claims(now time.Time, p testPrincipal, credential map[string]any) jwt.MapClaims {
	claims := jwt.MapClaims{
		"iss":             testIssuer,
		"sub":             p.subject,
		"aud":             testAudience,
		"organization_id": testOrg,
		"mcp_server_id":   p.mcpServerID,
		"iat":             now.Unix(),
		"exp":             now.Add(identity.MaxLifetime).Unix(),
		"jti":             uuid.NewString(),
		"version":         1,
	}
	if p.consent {
		claims["allowed_methods"] = []string{"server/discover", "initialize", "notifications/initialized", "ping", "tools/list"}
	}
	if credential != nil {
		claims["upstream_credential"] = credential
	}
	return claims
}

func (s *testSigner) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid
	token.Header["typ"] = identity.TokenType
	raw, err := token.SignedString(s.key)
	require.NoError(t, err)
	return raw
}

// fakeCredentialStore keeps session files in a temporary directory, for
// bridge tests on any Unix host. The Linux store has its own tests.
type fakeCredentialStore struct {
	root    string
	mu      sync.Mutex
	writes  int
	created int
	live    map[string]bool
}

func newFakeCredentialStore(t *testing.T) *fakeCredentialStore {
	t.Helper()
	return &fakeCredentialStore{root: t.TempDir(), live: map[string]bool{}}
}

func (f *fakeCredentialStore) createSession() (credentialDir, error) {
	dir, err := os.MkdirTemp(f.root, "s-")
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Join(dir, "home"), 0o700); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.created++
	f.live[dir] = true
	f.mu.Unlock()
	return &fakeCredentialDir{store: f, path: dir}, nil
}

func (f *fakeCredentialStore) Close() error { return nil }

func (f *fakeCredentialStore) counts() (created, writes, live int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created, f.writes, len(f.live)
}

type fakeCredentialDir struct {
	store *fakeCredentialStore
	path  string
}

func (d *fakeCredentialDir) writeToken(token string) error {
	tmp := filepath.Join(d.path, ".token-tmp")
	if err := os.WriteFile(tmp, []byte(token), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, d.tokenPath()); err != nil {
		return err
	}
	d.store.mu.Lock()
	d.store.writes++
	d.store.mu.Unlock()
	return nil
}

func (d *fakeCredentialDir) tokenPath() string { return filepath.Join(d.path, "token") }

func (d *fakeCredentialDir) homePath() string { return filepath.Join(d.path, "home") }

func (d *fakeCredentialDir) remove() error {
	d.store.mu.Lock()
	delete(d.store.live, d.path)
	d.store.mu.Unlock()
	return os.RemoveAll(d.path)
}
