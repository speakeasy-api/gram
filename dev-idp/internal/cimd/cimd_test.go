package cimd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/cimd"
)

// currentKeyID reads a kid out of an atomic.Value without an inline
// assertion, which the linter reads as unchecked.
func currentKeyID(v *atomic.Value) string {
	kid, _ := v.Load().(string)
	return kid
}

// publisher is a client hosting its own metadata document, counting reads of
// each piece so a test can tell what was fetched and what was reused.
type publisher struct {
	clientID     string
	docFetches   *atomic.Int32
	jwksFetches  *atomic.Int32
	currentKeyID *atomic.Value
}

func newPublisher(t *testing.T, inline bool) *publisher {
	t.Helper()

	var docFetches, jwksFetches atomic.Int32
	var keyID atomic.Value
	keyID.Store("key-1")

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	clientID := server.URL + "/client.json"

	jwksFor := func(kid string) map[string]any {
		return map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": kid,
				// Small but structurally valid; these tests care about fetch
				// counts and identity, not signature verification.
				"n": "sXchDaQe",
				"e": "AQAB",
			}},
		}
	}

	mux.HandleFunc("GET /client.json", func(w http.ResponseWriter, _ *http.Request) {
		docFetches.Add(1)
		doc := map[string]any{"client_id": clientID}
		if inline {
			doc["jwks"] = jwksFor(currentKeyID(&keyID))
		} else {
			doc["jwks_uri"] = server.URL + "/jwks.json"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	})

	mux.HandleFunc("GET /jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		jwksFetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwksFor(currentKeyID(&keyID)))
	})

	return &publisher{
		clientID:     clientID,
		docFetches:   &docFetches,
		jwksFetches:  &jwksFetches,
		currentKeyID: &keyID,
	}
}

// A jwks_uri means a second request, and without caching the key set that
// request would happen on every authentication rather than once per TTL.
func TestResolverCachesKeysBehindAJwksURI(t *testing.T) {
	t.Parallel()

	pub := newPublisher(t, false)
	r := cimd.NewResolver(http.DefaultClient, time.Minute)

	for range 5 {
		_, hasKeys, err := r.Keys(t.Context(), pub.clientID)
		require.NoError(t, err)
		require.True(t, hasKeys)
	}

	require.Equal(t, int32(1), pub.docFetches.Load(), "document should be fetched once")
	require.Equal(t, int32(1), pub.jwksFetches.Load(), "jwks_uri should be followed once")
}

func TestResolverCachesInlineKeys(t *testing.T) {
	t.Parallel()

	pub := newPublisher(t, true)
	r := cimd.NewResolver(http.DefaultClient, time.Minute)

	for range 5 {
		_, _, err := r.Keys(t.Context(), pub.clientID)
		require.NoError(t, err)
	}

	require.Equal(t, int32(1), pub.docFetches.Load())
	require.Zero(t, pub.jwksFetches.Load(), "inline keys need no second request")
}

// Rotation is the client republishing. Once the TTL lapses the new key is
// picked up without anything being edited on this side.
func TestResolverPicksUpRotatedKeysAfterTTL(t *testing.T) {
	t.Parallel()

	pub := newPublisher(t, false)
	r := cimd.NewResolver(http.DefaultClient, time.Millisecond)

	first, _, err := r.Keys(t.Context(), pub.clientID)
	require.NoError(t, err)
	require.Equal(t, "key-1", first.Keys[0].Kid)

	pub.currentKeyID.Store("key-2")
	time.Sleep(5 * time.Millisecond)

	second, _, err := r.Keys(t.Context(), pub.clientID)
	require.NoError(t, err)
	require.Equal(t, "key-2", second.Keys[0].Kid, "a republished key should take effect")
}

// A document that publishes no key material describes a public client, which
// is not an error the caller has to special-case.
func TestResolverReportsNoKeysWithoutError(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	clientID := server.URL + "/client.json"

	mux.HandleFunc("GET /client.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": clientID})
	})

	r := cimd.NewResolver(http.DefaultClient, time.Minute)
	_, hasKeys, err := r.Keys(t.Context(), clientID)
	require.NoError(t, err)
	require.False(t, hasKeys)
}

// A document claiming an identity that is not its own is refused, and the
// failure is distinguishable from the host being unreachable.
func TestResolverRejectsClientIDMismatch(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	clientID := server.URL + "/client.json"

	mux.HandleFunc("GET /client.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"client_id": "https://someone-else.example/client.json",
		})
	})

	r := cimd.NewResolver(http.DefaultClient, time.Minute)
	_, err := r.Document(t.Context(), clientID)
	require.ErrorIs(t, err, cimd.ErrClientIDMismatch)
}

// Errors must not be cached: a client fixing its document should not have to
// wait out a TTL.
func TestResolverDoesNotCacheFailures(t *testing.T) {
	t.Parallel()

	var fetches atomic.Int32
	var healthy atomic.Bool

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	clientID := server.URL + "/client.json"

	mux.HandleFunc("GET /client.json", func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		if !healthy.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": clientID})
	})

	r := cimd.NewResolver(http.DefaultClient, time.Minute)
	_, err := r.Document(t.Context(), clientID)
	require.Error(t, err)

	healthy.Store(true)
	_, err = r.Document(t.Context(), clientID)
	require.NoError(t, err, "a recovered document should resolve immediately")
	require.Equal(t, int32(2), fetches.Load())
}
