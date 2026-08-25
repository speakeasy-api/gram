// Integration tests for inbound CIMD (Client ID Metadata Documents) on the
// Platform MCP authorization server: a URL-shaped client_id presented to
// /platform-mcp/authorize is resolved by fetching the metadata document from
// an httptest TLS server the guardian policy is told to trust.

package platformmcp

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

type cimdDocServer struct {
	srv      *httptest.Server
	clientID string

	mu           sync.Mutex
	doc          map[string]any
	etag         string
	cacheControl string
	status       int

	// requests counts every request the document host received, so a test
	// can assert that a cached resolve costs no upstream call.
	requests atomic.Int64
}

func (ds *cimdDocServer) set(t *testing.T, mutate func(ds *cimdDocServer)) {
	t.Helper()

	ds.mu.Lock()
	defer ds.mu.Unlock()
	mutate(ds)
}

func startCIMDDocServer(t *testing.T) *cimdDocServer {
	t.Helper()

	ds := &cimdDocServer{
		srv:          nil,
		clientID:     "",
		mu:           sync.Mutex{},
		doc:          nil,
		etag:         "",
		cacheControl: "",
		status:       0,
		requests:     atomic.Int64{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/client.json", func(w http.ResponseWriter, r *http.Request) {
		ds.requests.Add(1)
		conditional := r.Header.Get("If-None-Match")

		ds.mu.Lock()
		defer ds.mu.Unlock()

		// Failure injection wins over revalidation: a host that has started
		// erroring does so whether or not the request was conditional.
		if ds.status != 0 {
			http.Error(w, "document host failure", ds.status)
			return
		}
		if ds.cacheControl != "" {
			w.Header().Set("Cache-Control", ds.cacheControl)
		}
		if ds.etag != "" && conditional == ds.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if ds.etag != "" {
			w.Header().Set("ETag", ds.etag)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(ds.doc); err != nil {
			t.Errorf("encode cimd document: %v", err)
		}
	})
	ds.srv = httptest.NewTLSServer(mux)
	t.Cleanup(ds.srv.Close)

	ds.clientID = ds.srv.URL + "/oauth/client.json"
	ds.doc = map[string]any{
		"client_id":                  ds.clientID,
		"client_name":                "CIMD Platform Client",
		"redirect_uris":              []any{"http://127.0.0.1:33418/callback"},
		"token_endpoint_auth_method": "none",
	}
	return ds
}

func (ds *cimdDocServer) certPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ds.srv.Certificate())
	return pool
}

// newTestCIMDOAuthHTTP builds a document server plus a Platform MCP
// authorization server whose guardian policy trusts that server's TLS
// certificate. The policy is the unsafe variant because the document host is
// a loopback httptest server, which the default policy's SSRF rules refuse.
func newTestCIMDOAuthHTTP(t *testing.T) (*OAuthHTTP, *cimdDocServer) {
	t.Helper()

	ds := startCIMDDocServer(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(ds.certPool()))
	require.NoError(t, err)

	base, err := url.Parse("https://gram.example")
	require.NoError(t, err)
	service, err := NewOAuthHTTP(OAuthHTTPConfig{
		BaseURL:        base,
		Cache:          &memoryCache{values: map[string]any{}},
		Store:          platformoauth.NewInMemoryStore(),
		Identity:       testIdentity{},
		Gate:           allowGate{},
		Authorizer:     allowAuthorizer{},
		Organizations:  testOrganizationSelector{organizations: []OrganizationOption{{ID: "org-1", Name: "Organization one"}}},
		Signer:         sessiontokens.NewSigner("test-key"),
		Encryption:     testCIMDEncryption(t),
		GuardianPolicy: policy,
	})
	require.NoError(t, err)
	return service, ds
}

func testCIMDEncryption(t *testing.T) *encryption.Client {
	t.Helper()

	client, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	return client
}

func doCIMDAuthorize(t *testing.T, service *OAuthHTTP, clientID, redirectURI string) *httptest.ResponseRecorder {
	t.Helper()

	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"},
		"code_challenge_method": {"S256"},
	}
	request := httptest.NewRequest(http.MethodGet, "/platform-mcp/authorize?"+query.Encode(), nil)
	response := httptest.NewRecorder()
	service.AuthorizeHandler().ServeHTTP(response, request)
	return response
}

func TestCIMDAuthorize_ResolvesAndPersistsClient(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)

	response := doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback")
	require.Equal(t, http.StatusFound, response.Code)
	require.Contains(t, response.Header().Get("Location"), "https://idp.example/authorize")

	client, err := service.store.GetClient(t.Context(), ds.clientID)
	require.NoError(t, err)
	require.True(t, client.IsCIMD())
	require.Equal(t, ds.clientID, client.MetadataURI)
	require.Equal(t, "CIMD Platform Client", client.Name)
	require.Empty(t, client.SecretHash, "a CIMD client is public by construction")
	require.Equal(t, int64(1), ds.requests.Load())
}

func TestCIMDAuthorize_ServesCachedDocumentWithoutRefetching(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)
	ds.set(t, func(ds *cimdDocServer) { ds.cacheControl = "max-age=600" })

	require.Equal(t, http.StatusFound, doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback").Code)
	require.Equal(t, http.StatusFound, doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback").Code)

	require.Equal(t, int64(1), ds.requests.Load(), "second authorize must be served from the stored row")
}

func TestCIMDAuthorize_LoopbackRedirectPortMayVary(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)

	// RFC 8252 §7.3: a native client binds an OS-assigned ephemeral port per
	// invocation, so the port registered in the document cannot be matched
	// byte-exactly.
	response := doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:51099/callback")
	require.Equal(t, http.StatusFound, response.Code)

	// Every other component still has to match.
	response = doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:51099/other")
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client"`)
}

func TestCIMDAuthorize_NonLoopbackRedirectStaysExact(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)
	// Same-origin with the document URL, as Gram's origin-binding policy
	// requires, and https rather than http so it is not an RFC 8252 §7.3
	// loopback redirect: the variable-port exception must not apply.
	ds.set(t, func(ds *cimdDocServer) {
		ds.doc["redirect_uris"] = []any{ds.srv.URL + "/callback"}
	})

	docURL, err := url.Parse(ds.srv.URL)
	require.NoError(t, err)
	otherPort := *docURL
	otherPort.Host = docURL.Hostname() + ":" + docURL.Port() + "9"
	otherPort.Path = "/callback"

	response := doCIMDAuthorize(t, service, ds.clientID, otherPort.String())
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client"`)
}

func TestCIMDAuthorize_DocumentFetchFailureIsRetryable(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)
	ds.set(t, func(ds *cimdDocServer) { ds.status = http.StatusInternalServerError })

	response := doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Contains(t, response.Body.String(), `"temporarily_unavailable"`)
	require.NotContains(t, response.Body.String(), ds.srv.URL, "the wire response must not echo transport detail")

	// Retryable means the next attempt actually works: the failure left no
	// negative cache behind, so a recovered host resolves normally.
	ds.set(t, func(ds *cimdDocServer) { ds.status = 0 })
	require.Equal(t, http.StatusFound, doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback").Code)
	require.Equal(t, int64(2), ds.requests.Load())
}

func TestCIMDAuthorize_OversizedClientIDDeniedBeforeAnyLookup(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)
	oversized := ds.srv.URL + "/oauth/" + strings.Repeat("x", admission.MaxClientIDLength) + ".json"

	response := doCIMDAuthorize(t, service, oversized, "http://127.0.0.1:33418/callback")
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client"`)
	require.Zero(t, ds.requests.Load(), "an oversized client_id must not reach the document host")
}

func TestCIMDAuthorize_DocumentClientIDMismatchRejected(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)
	ds.set(t, func(ds *cimdDocServer) { ds.doc["client_id"] = "https://elsewhere.example/oauth/client.json" })

	response := doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback")
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client_metadata"`)
}

func TestCIMDAuthorize_ConfidentialDocumentRejected(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)
	ds.set(t, func(ds *cimdDocServer) {
		ds.doc["token_endpoint_auth_method"] = "client_secret_basic"
	})

	response := doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback")
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client_metadata"`)
}

func TestCIMDToken_ClientPresentingSecretRejected(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)
	require.Equal(t, http.StatusFound, doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback").Code)

	// A CIMD client is public: presenting credentials means something other
	// than the legitimate client is talking, so the token leg must refuse it
	// rather than treating the secret as absent.
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ds.clientID},
		"client_secret": {"not-a-real-secret"},
		"code":          {"whatever"},
	}
	request := httptest.NewRequest(http.MethodPost, "/platform-mcp/token", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	service.TokenHandler().ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), `"invalid_client"`)
}

func TestAuthorizationServerMetadata_AdvertisesCIMDOnlyWhenResolvable(t *testing.T) {
	t.Parallel()

	withCIMD, _ := newTestCIMDOAuthHTTP(t)
	require.True(t, authorizationServerMetadataFlag(t, withCIMD), "a resolvable AS advertises CIMD support")

	// No guardian policy means no resolver: advertising support would route
	// spec-compliant clients into a guaranteed-failure flow instead of
	// letting them fall back to dynamic client registration.
	require.False(t, authorizationServerMetadataFlag(t, newTestOAuthHTTP(t)))
}

func authorizationServerMetadataFlag(t *testing.T, service *OAuthHTTP) bool {
	t.Helper()

	response := httptest.NewRecorder()
	service.AuthorizationServerHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/platform-mcp", nil))
	require.Equal(t, http.StatusOK, response.Code)

	// Absence is how RFC 8414 metadata says "unsupported", so the member is
	// decoded as a pointer: a literal false would be a different answer.
	var metadata struct {
		Supported *bool `json:"client_id_metadata_document_supported"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &metadata))
	if metadata.Supported == nil {
		return false
	}
	return *metadata.Supported
}

// notModifiedResolver stands in for the document fetcher on the one branch
// the httptest server cannot reach from a handler test: the cache floor is
// five minutes, so a stored document cannot be made stale mid-test to force
// a conditional request.
type notModifiedResolver struct {
	ttl  time.Duration
	etag string
}

func (r notModifiedResolver) Resolve(_ context.Context, _ string, _ cimd.CacheState) (*cimd.CacheResult, error) {
	return &cimd.CacheResult{Outcome: cimd.CacheOutcomeNotModified, Document: nil, ETag: r.etag, TTL: r.ttl}, nil
}

func TestCIMDResolve_NotModifiedRefreshesStoredCacheOnly(t *testing.T) {
	t.Parallel()

	service, ds := newTestCIMDOAuthHTTP(t)
	require.Equal(t, http.StatusFound, doCIMDAuthorize(t, service, ds.clientID, "http://127.0.0.1:33418/callback").Code)

	service.cimd = notModifiedResolver{ttl: 30 * time.Minute, etag: `"v2"`}
	client, err := service.resolveClient(t.Context(), ds.clientID, resolveClientCIMD)
	require.NoError(t, err)

	// A 304 confirms the stored document, so only the validator and the
	// expiry move; the metadata it vouches for is left alone.
	require.Equal(t, `"v2"`, client.ETag)
	require.Equal(t, "CIMD Platform Client", client.Name)
	require.Equal(t, []string{"http://127.0.0.1:33418/callback"}, client.RedirectURIs)
	require.NotNil(t, client.CacheExpiresAt)
}
