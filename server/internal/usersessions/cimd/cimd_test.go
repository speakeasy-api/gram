package cimd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

func TestIsClientIDURL(t *testing.T) {
	t.Parallel()

	require.True(t, IsClientIDURL("https://client.example.com/oauth/client.json"))
	require.False(t, IsClientIDURL("client_5f2c9a"))
	require.False(t, IsClientIDURL("http://client.example.com/client.json"))
	require.False(t, IsClientIDURL("HTTPS://client.example.com/client.json"), "no normalization: scheme must be lowercase")
	require.False(t, IsClientIDURL("https://"))
}

// TestDocument_DeclaredAuthMethod pins the absent-member rule that decides
// what gets persisted: a document that names no method has declared "none",
// which is a different claim from the NULL stored on rows that predate the
// column, so the two must never be conflated by a caller writing the row.
func TestDocument_DeclaredAuthMethod(t *testing.T) {
	t.Parallel()

	absent := &Document{}
	require.Equal(t, "none", absent.DeclaredAuthMethod(), "an absent member declares a public client")

	explicit := &Document{TokenEndpointAuthMethod: "none"}
	require.Equal(t, "none", explicit.DeclaredAuthMethod())

	// Carried verbatim rather than filtered: whether a method is acceptable
	// is validateDocument's decision, and this method reports only what the
	// document said.
	other := &Document{TokenEndpointAuthMethod: "private_key_jwt"}
	require.Equal(t, "private_key_jwt", other.DeclaredAuthMethod())
}

// newDocServer starts a TLS server whose /client.json responds via handler
// and returns the server plus a resolver whose fetch client trusts its
// certificate — the same injection pattern production uses with a
// guardian-built client.
func newDocServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Resolver) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return srv, newResolver(newFetchClientFrom(srv.Client()), testenv.NewMeterProvider(t), testenv.NewLogger(t))
}

// serveDocumentJSON responds 200 application/json with the document derived
// from the request URL by fn.
func serveDocumentJSON(t *testing.T, fn func(clientID string) map[string]any) (*httptest.Server, *Resolver, string) {
	t.Helper()

	var clientID string
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fn(clientID)); err != nil {
			t.Errorf("encode document: %v", err)
		}
	})
	clientID = srv.URL + "/client.json"
	return srv, resolver, clientID
}

func validDocumentJSON(clientID string) map[string]any {
	return map[string]any{
		"client_id":                  clientID,
		"client_name":                "CIMD Test Client",
		"redirect_uris":              []string{"http://127.0.0.1:3000/callback"},
		"token_endpoint_auth_method": "none",
	}
}

// noCache is the cache state of a client_id nothing has been stored for. It
// forces an unconditional fetch, which is what every test that is not about
// the cache policy wants.
var noCache = CacheState{ExpiresAt: time.Time{}, ETag: ""}

func TestResolve_HappyPath(t *testing.T) {
	t.Parallel()

	_, resolver, clientID := serveDocumentJSON(t, validDocumentJSON)

	result, err := resolver.Resolve(t.Context(), clientID, noCache)
	require.NoError(t, err)
	require.Equal(t, CacheOutcomeRefreshed, result.Outcome)
	require.Equal(t, clientID, result.Document.ClientID)
	require.Equal(t, "CIMD Test Client", result.Document.ClientName)
	require.Equal(t, []string{"http://127.0.0.1:3000/callback"}, result.Document.RedirectURIs)
}

func TestResolve_Non200Rejected(t *testing.T) {
	t.Parallel()

	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)
	require.ErrorContains(t, err, "status 404")
}

func TestResolve_RedirectNotFollowed(t *testing.T) {
	t.Parallel()

	// -02 §5: the fetch MUST NOT follow redirects. The 302 must surface as
	// a fetch failure even though its target would serve a valid document.
	var clientID string
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target.json" {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(validDocumentJSON(clientID)); err != nil {
				t.Errorf("encode document: %v", err)
			}
			return
		}
		http.Redirect(w, r, "/target.json", http.StatusFound)
	})
	clientID = srv.URL + "/client.json"

	_, err := resolver.Resolve(t.Context(), clientID, noCache)
	require.ErrorContains(t, err, "status 302")
}

func TestResolve_OversizedDocumentRejected(t *testing.T) {
	t.Parallel()

	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"padding":%q`, strings.Repeat("a", maxDocumentBytes+1)); err != nil {
			t.Errorf("write oversized document: %v", err)
		}
	})

	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)
	require.ErrorContains(t, err, "byte limit")
}

func TestResolve_InvalidJSONRejected(t *testing.T) {
	t.Parallel()

	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write document: %v", err)
		}
	})

	// Deliberately NOT an OAuthError: a distinguishable "reachable but not
	// JSON" outcome would let unauthenticated callers probe external hosts
	// through Gram, so it reports like any other fetch failure.
	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)
	require.ErrorContains(t, err, "parse client metadata document")
	var oauthErr *oauthwire.Error
	require.NotErrorAs(t, err, &oauthErr)
}

func TestResolve_DocumentClientIDMismatchRejected(t *testing.T) {
	t.Parallel()

	_, resolver, clientID := serveDocumentJSON(t, func(clientID string) map[string]any {
		doc := validDocumentJSON(clientID)
		doc["client_id"] = clientID + "?other"
		return doc
	})

	_, err := resolver.Resolve(t.Context(), clientID, noCache)
	requireOAuthError(t, err, "invalid_client_metadata")
}

func TestResolve_InvalidClientIDURLNoFetch(t *testing.T) {
	t.Parallel()

	requests := 0
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
	})

	_, err := resolver.Resolve(t.Context(), srv.URL, noCache) // no path component
	requireOAuthError(t, err, "invalid_request")
	require.Zero(t, requests, "syntactically invalid client_id must never be fetched")
}

func TestResolve_ProductionPolicyBlocksLoopback(t *testing.T) {
	t.Parallel()

	// The production guardian policy (default CIDR blocklist) must refuse
	// to fetch documents from RFC 6890 special-use addresses — httptest
	// binds 127.0.0.1, so the dial itself is denied (-02 §8.6).
	srv, _ := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("SSRF-guarded client must not reach a loopback document host")
	})

	resolver := NewResolver(guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)), testenv.NewMeterProvider(t), testenv.NewLogger(t))
	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)
	require.ErrorContains(t, err, "request document")
}
