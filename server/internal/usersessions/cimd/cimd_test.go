package cimd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

func TestIsClientIDURL(t *testing.T) {
	t.Parallel()

	require.True(t, IsClientIDURL("https://client.example.com/oauth/client.json"))
	require.False(t, IsClientIDURL("client_5f2c9a"))
	require.False(t, IsClientIDURL("http://client.example.com/client.json"))
	require.False(t, IsClientIDURL("HTTPS://client.example.com/client.json"), "no normalization: scheme must be lowercase")
	require.False(t, IsClientIDURL("https://"))
}

// newDocServer starts a TLS server whose /client.json responds via handler
// and returns the server plus a fetch client that trusts its certificate —
// the same injection pattern production uses with a guardian-built client.
func newDocServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return srv, newFetchClientFrom(srv.Client())
}

// serveDocumentJSON responds 200 application/json with the document derived
// from the request URL by fn.
func serveDocumentJSON(t *testing.T, fn func(clientID string) map[string]any) (*httptest.Server, *http.Client, string) {
	t.Helper()

	var clientID string
	srv, client := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fn(clientID)); err != nil {
			t.Errorf("encode document: %v", err)
		}
	})
	clientID = srv.URL + "/client.json"
	return srv, client, clientID
}

func validDocumentJSON(clientID string) map[string]any {
	return map[string]any{
		"client_id":                  clientID,
		"client_name":                "CIMD Test Client",
		"redirect_uris":              []string{"http://127.0.0.1:3000/callback"},
		"token_endpoint_auth_method": "none",
	}
}

func TestResolve_HappyPath(t *testing.T) {
	t.Parallel()

	_, client, clientID := serveDocumentJSON(t, validDocumentJSON)

	doc, err := Resolve(t.Context(), client, clientID)
	require.NoError(t, err)
	require.Equal(t, clientID, doc.ClientID)
	require.Equal(t, "CIMD Test Client", doc.ClientName)
	require.Equal(t, []string{"http://127.0.0.1:3000/callback"}, doc.RedirectURIs)
}

func TestResolve_Non200Rejected(t *testing.T) {
	t.Parallel()

	srv, client := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	_, err := Resolve(t.Context(), client, srv.URL+"/client.json")
	require.ErrorContains(t, err, "status 404")
}

func TestResolve_RedirectNotFollowed(t *testing.T) {
	t.Parallel()

	// -02 §5: the fetch MUST NOT follow redirects. The 302 must surface as
	// a fetch failure even though its target would serve a valid document.
	var clientID string
	srv, client := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
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

	_, err := Resolve(t.Context(), client, clientID)
	require.ErrorContains(t, err, "status 302")
}

func TestResolve_OversizedDocumentRejected(t *testing.T) {
	t.Parallel()

	srv, client := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w, `{"padding":%q`, strings.Repeat("a", maxDocumentBytes+1)); err != nil {
			t.Errorf("write oversized document: %v", err)
		}
	})

	_, err := Resolve(t.Context(), client, srv.URL+"/client.json")
	require.ErrorContains(t, err, "byte limit")
}

func TestResolve_InvalidJSONRejected(t *testing.T) {
	t.Parallel()

	srv, client := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write document: %v", err)
		}
	})

	// Deliberately NOT an OAuthError: a distinguishable "reachable but not
	// JSON" outcome would let unauthenticated callers probe external hosts
	// through Gram, so it reports like any other fetch failure.
	_, err := Resolve(t.Context(), client, srv.URL+"/client.json")
	require.ErrorContains(t, err, "parse client metadata document")
	var oauthErr *usersessions.OAuthError
	require.NotErrorAs(t, err, &oauthErr)
}

func TestResolve_DocumentClientIDMismatchRejected(t *testing.T) {
	t.Parallel()

	_, client, clientID := serveDocumentJSON(t, func(clientID string) map[string]any {
		doc := validDocumentJSON(clientID)
		doc["client_id"] = clientID + "?other"
		return doc
	})

	_, err := Resolve(t.Context(), client, clientID)
	requireOAuthError(t, err, "invalid_client_metadata")
}

func TestResolve_InvalidClientIDURLNoFetch(t *testing.T) {
	t.Parallel()

	requests := 0
	srv, client := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
	})

	_, err := Resolve(t.Context(), client, srv.URL) // no path component
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

	client := NewFetchClient(guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)))
	_, err := Resolve(t.Context(), client, srv.URL+"/client.json")
	require.ErrorContains(t, err, "request document")
}
