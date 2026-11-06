package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	mockoidc "github.com/speakeasy-api/gram/mock-oidc"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/cataloglocalclient"
	"github.com/speakeasy-api/gram/server/internal/catalogsource"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"golang.org/x/oauth2"
)

type catalogVerifierFunc func(context.Context, string) error

func (f catalogVerifierFunc) Verify(ctx context.Context, token string) error { return f(ctx, token) }

type catalogSourceFunc func(context.Context) (mcpregistry.CatalogBundle, error)

func (f catalogSourceFunc) Fetch(ctx context.Context) (mcpregistry.CatalogBundle, error) {
	return f(ctx)
}

type catalogUnreadBody struct{ read bool }

func (b *catalogUnreadBody) Read([]byte) (int, error) { b.read = true; return 0, io.EOF }
func (b *catalogUnreadBody) Close() error             { return nil }

func catalogSession(t *testing.T, s *Service, subject string) string {
	t.Helper()
	id, err := s.sessions.Store(t.Context(), StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: subject, HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	return id
}
func catalogRequest(t *testing.T, handler http.Handler, session string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogSyncPath, body)
	r.Header.Set("Origin", "https://admin.example.test")
	r.Header.Set("Content-Type", "application/json")
	if session != "" {
		r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
	}
	w := httptest.NewRecorder()
	SessionMiddleware(handler).ServeHTTP(w, r)
	return w
}

func TestCatalogSyncSafetyBoundary(t *testing.T) {
	t.Parallel()
	_, s, _ := newTestAdminService(t)
	session := catalogSession(t, s, "sub-admin")
	calls := 0
	source := catalogSourceFunc(func(context.Context) (mcpregistry.CatalogBundle, error) {
		calls++
		return mcpregistry.CatalogBundle{}, nil
	})
	for _, env := range []string{"prod", "production", "", "preview", "staging"} {
		body := &catalogUnreadBody{}
		response := catalogRequest(t, s.CatalogSyncHandler(env, source, &mcpregistry.CatalogAssetStore{}), session, body)
		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		require.False(t, body.read)
		require.Zero(t, calls)
	}
	for _, credential := range []string{"", "not-a-session", catalogSession(t, s, "wrong-subject")} {
		response := catalogRequest(t, s.CatalogSyncHandler("local", source, &mcpregistry.CatalogAssetStore{}), credential, bytes.NewBufferString(`{}`))
		require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
		require.Zero(t, calls)
	}
	// An export workload bearer token is never accepted as an Admin session.
	bearerRequest := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogSyncPath, bytes.NewBufferString(`{}`))
	bearerRequest.Header.Set("Origin", "https://admin.example.test")
	bearerRequest.Header.Set("Authorization", "Bearer synthetic-export-token")
	bearerResponse := httptest.NewRecorder()
	SessionMiddleware(s.CatalogSyncHandler("local", source, &mcpregistry.CatalogAssetStore{})).ServeHTTP(bearerResponse, bearerRequest)
	require.Equal(t, http.StatusUnauthorized, bearerResponse.Code)
	require.Zero(t, calls)
	for _, body := range []string{`{"environment":"local"}`, `{"database_url":"postgres://example.invalid"}`, `{"source_url":"https://example.invalid"}`, `{"assets_uri":"gs://example.invalid"}`} {
		response := catalogRequest(t, s.CatalogSyncHandler("local", source, &mcpregistry.CatalogAssetStore{}), session, bytes.NewBufferString(body))
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Zero(t, calls)
	}
	handler := SessionMiddleware(s.CatalogSyncHandler("dev", source, &mcpregistry.CatalogAssetStore{}))
	r := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+CatalogSyncPath, bytes.NewBufferString(`{}`))
	r.Header.Set("Origin", "https://untrusted.example.test")
	r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, r)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Zero(t, calls)
}

func TestCatalogSyncHTTPAssetsAndCatchUp(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestAdminService(t)
	_, source, sourceDB := newTestAdminService(t)
	tp := testenv.NewTracerProvider(t)
	sourceBase, err := url.Parse("https://source.example.test")
	require.NoError(t, err)
	destBase, err := url.Parse("https://destination.example.test")
	require.NoError(t, err)
	source.registry.WithAssetBaseURL(sourceBase)
	s.registry.WithAssetBaseURL(destBase)
	srcBlob := assetstest.NewTestBlobStore(t)
	dstBlob := assetstest.NewTestBlobStore(t)
	sourceAssets := &mcpregistry.CatalogAssetStore{Registry: source.registry, Images: assets.NewPlatformService(s.logger, tp, guardian.NewDefaultPolicy(tp), sourceDB, srcBlob), Blob: srcBlob, Logger: s.logger, Operator: "operator@example.test"}
	destAssets := &mcpregistry.CatalogAssetStore{Registry: s.registry, Images: assets.NewPlatformService(s.logger, tp, guardian.NewDefaultPolicy(tp), db, dstBlob), Blob: dstBlob, Logger: s.logger, Operator: ""}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`)
	src, err := sourceAssets.StoreCatalogAsset(ctx, svg)
	require.NoError(t, err)
	raw, err := mcpregistry.WithCatalogIcons(json.RawMessage(`{"server":{"name":"example.test/new","description":"Demo","version":"1"}}`), mcpregistry.CatalogIcons{Default: &mcpregistry.CatalogIcon{Src: src, MimeType: "image/svg+xml", Sizes: []string{"any"}}, Dark: nil})
	require.NoError(t, err)
	_, err = source.registry.Create(ctx, raw)
	require.NoError(t, err)
	sourceHTTP := httptest.NewTLSServer(catalogsource.NewExportHandler(func(ctx context.Context) (mcpregistry.CatalogBundle, error) {
		return source.registry.ExportCatalog(ctx, sourceAssets)
	}, catalogVerifierFunc(func(_ context.Context, token string) error {
		if token != "synthetic-export-token" {
			return errors.New("unauthorized")
		}
		return nil
	})))
	t.Cleanup(sourceHTTP.Close)
	fetcher, err := catalogsource.NewClient(sourceHTTP.URL+catalogsource.ExportPath, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "synthetic-export-token"}), sourceHTTP.Client())
	require.NoError(t, err)
	handler := s.CatalogSyncHandler("local", fetcher, destAssets)
	session := catalogSession(t, s, "sub-admin")
	destinationHTTP := httptest.NewTLSServer(SessionMiddleware(handler))
	t.Cleanup(destinationHTTP.Close)
	send := func() *httptest.ResponseRecorder {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, destinationHTTP.URL+CatalogSyncPath, bytes.NewBufferString(`{}`))
		require.NoError(t, err)
		req.Header.Set("Origin", destinationHTTP.URL)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
		res, err := destinationHTTP.Client().Do(req)
		require.NoError(t, err)
		defer func() { require.NoError(t, res.Body.Close()) }()
		body, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		recorder.WriteHeader(res.StatusCode)
		_, err = recorder.Write(body)
		require.NoError(t, err)
		return recorder
	}
	response := send()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	entry, err := s.registry.GetByName(ctx, "example.test/new")
	require.NoError(t, err)
	icons, err := mcpregistry.ParseCatalogIcons(entry.Data)
	require.NoError(t, err)
	require.Contains(t, icons.Default.Src, "destination.example.test")
	image, err := destAssets.ReadCatalogAsset(ctx, icons.Default.Src)
	require.NoError(t, err)
	require.NotEmpty(t, image)
	edited, err := s.registry.Save(ctx, entry.ID, mcpregistry.Token(entry), bytes.Replace(entry.Data, []byte(`"Demo"`), []byte(`"Testing edit"`), 1))
	require.NoError(t, err)
	_, err = source.registry.Create(ctx, json.RawMessage(`{"server":{"name":"example.test/later","description":"Later","version":"1"}}`))
	require.NoError(t, err)
	for range 2 {
		response = send()
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		got, err := s.registry.Get(ctx, edited.ID)
		require.NoError(t, err)
		require.Equal(t, edited.Data, got.Data)
		_, err = s.registry.GetByName(ctx, "example.test/later")
		require.NoError(t, err)
	}
	invalid := catalogSourceFunc(func(context.Context) (mcpregistry.CatalogBundle, error) {
		return mcpregistry.CatalogBundle{Version: 999}, nil
	})
	response = catalogRequest(t, s.CatalogSyncHandler("local", invalid, destAssets), session, bytes.NewBufferString(`{}`))
	require.NotEqual(t, http.StatusOK, response.Code)
}

//nolint:paralleltest // Installs a test-only trusted TLS root in the default transport used by the local CLI.
func TestCatalogSyncAutomaticLocalLogin(t *testing.T) {
	ctx, s, db := newTestAdminService(t)
	tp := testenv.NewTracerProvider(t)
	destinationMux := goahttp.NewMuxer()
	destination := httptest.NewTLSServer(SessionMiddleware(destinationMux))
	t.Cleanup(destination.Close)
	providerMux := http.NewServeMux()
	providerHTTP := httptest.NewServer(providerMux)
	t.Cleanup(providerHTTP.Close)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	cfg := &mockoidc.Config{Provider: mockoidc.ProviderConfig{
		Users:        []mockoidc.User{{Email: "operator@example.test", Name: "Test Operator", HD: testAdminHD, EmailVerified: true}},
		OAuthClients: []mockoidc.OAuthClient{{ClientID: "catalog-test", ClientSecret: "synthetic-client-secret", Name: "Catalog test", RedirectURIs: []string{destination.URL + "/admin/auth.callback"}}},
	}}
	provider, err := mockoidc.NewProvider(cfg, s.logger, providerHTTP.URL, key)
	require.NoError(t, err)
	providerMux.Handle("/", mockoidc.NewServer(provider, s.logger).Handler())
	discovered, err := oidc.NewProvider(ctx, providerHTTP.URL)
	require.NoError(t, err)
	policy, err := guardian.NewUnsafePolicy(tp, nil)
	require.NoError(t, err)
	s.oidc = NewOIDCClient(OIDCClientOptions{HTTPClient: policy.Client(), ClientID: "catalog-test", ClientSecret: "synthetic-client-secret", RedirectURL: destination.URL + "/admin/auth.callback", AllowedHDs: []string{testAdminHD}, Provider: discovered})
	s.verifier = NewVerifier(s.logger, s.sessions, s.oidc, s.verifier.locks)
	Attach(destinationMux, s)
	blob := assetstest.NewTestBlobStore(t)
	store := &mcpregistry.CatalogAssetStore{Registry: s.registry, Images: assets.NewPlatformService(s.logger, tp, policy, db, blob), Blob: blob, Logger: s.logger, Operator: ""}
	source := catalogSourceFunc(func(context.Context) (mcpregistry.CatalogBundle, error) {
		return mcpregistry.CatalogBundle{Version: 1, Entries: []json.RawMessage{json.RawMessage(`{"server":{"name":"example.test/automatic","description":"Automatic","version":"1"}}`)}}, nil
	})
	destinationMux.Handle(http.MethodPost, CatalogSyncPath, s.CatalogSyncHandler("local", source, store).ServeHTTP)
	roots := x509.NewCertPool()
	roots.AddCert(destination.Certificate())
	previous := http.DefaultTransport
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok)
	transport := defaultTransport.Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous; transport.CloseIdleConnections() })
	result, err := cataloglocalclient.Sync(ctx, destination.URL, providerHTTP.URL, "operator@example.test")
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/automatic"}, result.Created)
	_, err = s.registry.GetByName(ctx, "example.test/automatic")
	require.NoError(t, err)
	result, err = cataloglocalclient.Sync(ctx, destination.URL, providerHTTP.URL, "operator@example.test")
	require.NoError(t, err)
	require.Equal(t, []string{"example.test/automatic"}, result.Conflicts)
}
