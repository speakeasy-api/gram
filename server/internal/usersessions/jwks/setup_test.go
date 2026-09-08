package jwks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Redis: true})
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

// newTestLimiter returns a Redis-backed refresh limiter namespaced uniquely
// per test invocation, so neither parallel tests nor repeated runs against a
// reused Redis can inherit another run's bucket state.
func newTestLimiter(t *testing.T, rate ratelimit.Rate) *ratelimit.Limiter {
	t.Helper()

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	return ratelimit.New(ratelimit.NewRedisStore(client), string(testenv.NewCacheSuffix(t, "jwks")), rate)
}

// testKey generates an ES256 key pair and returns the public half as a JWK
// carrying kid. ECDSA is used over RSA purely for generation speed.
func testKey(t *testing.T, kid string) jose.JSONWebKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return jose.JSONWebKey{
		Key:       key.Public(),
		KeyID:     kid,
		Algorithm: "ES256",
		Use:       "sig",
	}
}

// keySetJSON marshals keys into a JWK Set document.
func keySetJSON(t *testing.T, keys ...jose.JSONWebKey) []byte {
	t.Helper()

	body, err := json.Marshal(jose.JSONWebKeySet{Keys: keys})
	require.NoError(t, err)
	return body
}

// keySetServer is an httptest TLS host serving a swappable key set document
// with optional ETag revalidation, counting the fetches it actually serves.
type keySetServer struct {
	server *httptest.Server

	mu      sync.Mutex
	body    []byte
	etag    string
	header  http.Header
	status  int
	fetches int
}

// newKeySetServer starts a TLS key set host serving body. The server is
// closed with the test.
func newKeySetServer(t *testing.T, body []byte) *keySetServer {
	t.Helper()

	s := &keySetServer{
		server:  nil,
		mu:      sync.Mutex{},
		body:    body,
		etag:    "",
		header:  http.Header{},
		status:  0,
		fetches: 0,
	}
	s.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.fetches++

		if s.status != 0 {
			w.WriteHeader(s.status)
			return
		}
		for name, values := range s.header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		if s.etag != "" {
			w.Header().Set("ETag", s.etag)
			if r.Header.Get("If-None-Match") == s.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(s.body); err != nil {
			// t.Error is goroutine-safe where require's FailNow is not.
			t.Errorf("write key set response: %v", err)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

// URL is the key set document URL on this host.
func (s *keySetServer) URL() string {
	return s.server.URL + "/jwks.json"
}

// SetBody swaps the served document, simulating an upstream key rotation.
func (s *keySetServer) SetBody(body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
}

// SetETag sets the validator served and honoured by the host.
func (s *keySetServer) SetETag(etag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.etag = etag
}

// SetStatus makes the host answer every request with the given status and no
// body; zero restores normal serving.
func (s *keySetServer) SetStatus(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// Fetches reports how many requests reached the host.
func (s *keySetServer) Fetches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetches
}

// resolverFor builds a Resolver whose fetch client trusts the test server's
// self-signed certificate, mirroring the production client's no-redirect
// policy.
func resolverFor(t *testing.T, s *keySetServer) *Resolver {
	t.Helper()

	return newResolver(newFetchClientFrom(s.server.Client()), testenv.NewMeterProvider(t), testenv.NewLogger(t))
}

// remoteSourceFor returns a remote Source for the test server's key set URL.
func remoteSourceFor(t *testing.T, s *keySetServer) Source {
	t.Helper()

	source, err := NewRemoteSource(s.URL())
	require.NoError(t, err)
	return source
}
