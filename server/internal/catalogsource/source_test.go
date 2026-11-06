package catalogsource

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/option"
)

type validatorFunc func(context.Context, string, string) (*idtoken.Payload, error)

func (f validatorFunc) Validate(c context.Context, t, a string) (*idtoken.Payload, error) {
	return f(c, t, a)
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func payload() idtoken.Payload {
	return idtoken.Payload{Issuer: "https://accounts.google.com", Audience: "catalog-audience", Subject: "123456789012345678901", Expires: time.Now().Add(time.Hour).Unix()}
}

func TestVerifierClaims(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*idtoken.Payload)
	}{
		{"issuer", func(p *idtoken.Payload) { p.Issuer = "https://evil.test" }},
		{"audience", func(p *idtoken.Payload) { p.Audience = "wrong" }},
		{"subject", func(p *idtoken.Payload) { p.Subject = "999" }},
		{"expiry", func(p *idtoken.Payload) { p.Expires = time.Now().Unix() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p := payload()
			test.change(&p)
			v, err := NewGoogleVerifier(t.Context(), "catalog-audience", []string{payload().Subject}, validatorFunc(func(_ context.Context, _ string, a string) (*idtoken.Payload, error) {
				require.Equal(t, "catalog-audience", a)
				return &p, nil
			}))
			require.NoError(t, err)
			require.Error(t, v.Verify(t.Context(), "token"))
		})
	}
	for _, subjects := range [][]string{nil, {}, {"email@example.test"}, {""}, {"-1"}} {
		_, err := NewGoogleVerifier(t.Context(), "audience", subjects, nil)
		require.Error(t, err)
	}
	_, err := NewGoogleVerifier(t.Context(), "", []string{"123"}, nil)
	require.Error(t, err)
	p := payload()
	subjects := []string{p.Subject}
	v, err := NewGoogleVerifier(t.Context(), p.Audience, subjects, validatorFunc(func(context.Context, string, string) (*idtoken.Payload, error) { return &p, nil }))
	require.NoError(t, err)
	subjects[0] = "changed"
	require.NoError(t, v.Verify(t.Context(), "token"))
}

func TestGoogleSignatureContract(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	b64 := base64.RawURLEncoding.EncodeToString
	jwks, err := json.Marshal(map[string]any{"keys": []any{map[string]string{"kid": "test-key", "kty": "RSA", "alg": "RS256", "use": "sig", "n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes())}}})
	require.NoError(t, err)
	google, err := idtoken.NewValidator(t.Context(), option.WithHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "https://www.googleapis.com/oauth2/v3/certs", r.URL.String())
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(jwks)))}, nil
	})}))
	require.NoError(t, err)
	v, err := NewGoogleVerifier(t.Context(), payload().Audience, []string{payload().Subject}, google)
	require.NoError(t, err)
	sign := func(p idtoken.Payload, signingKey *rsa.PrivateKey) string {
		raw, e := json.Marshal(p)
		require.NoError(t, e)
		content := b64([]byte(`{"alg":"RS256","kid":"test-key","typ":"JWT"}`)) + "." + b64(raw)
		digest := sha256.Sum256([]byte(content))
		sig, e := rsa.SignPKCS1v15(rand.Reader, signingKey, crypto.SHA256, digest[:])
		require.NoError(t, e)
		return content + "." + b64(sig)
	}
	require.NoError(t, v.Verify(t.Context(), sign(payload(), key)))
	for _, mutate := range []func(*idtoken.Payload){func(p *idtoken.Payload) { p.Issuer = "evil" }, func(p *idtoken.Payload) { p.Audience = "evil" }, func(p *idtoken.Payload) { p.Subject = "999" }, func(p *idtoken.Payload) { p.Expires = 1 }} {
		p := payload()
		mutate(&p)
		require.Error(t, v.Verify(t.Context(), sign(p, key)))
	}
	wrong, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	require.Error(t, v.Verify(t.Context(), sign(payload(), wrong)))
	require.Error(t, v.Verify(t.Context(), "not.a.jwt"))
}

func TestExportAuthentication(t *testing.T) {
	t.Parallel()
	p := payload()
	v, err := NewGoogleVerifier(t.Context(), p.Audience, []string{p.Subject}, validatorFunc(func(_ context.Context, token, _ string) (*idtoken.Payload, error) {
		if token != "good" {
			return nil, errors.New("secret")
		}
		return &p, nil
	}))
	require.NoError(t, err)
	calls := 0
	handler := NewExportHandler(func(context.Context) (mcpregistry.CatalogBundle, error) {
		calls++
		return mcpregistry.CatalogBundle{Version: 1}, nil
	}, v)
	for _, auth := range []string{"", "Basic good", "Bearer bad", "Bearer ", "Bearer good extra", "Bearer " + strings.Repeat("x", maxTokenBytes+1)} {
		r := httptest.NewRequest(http.MethodGet, ExportPath, nil)
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, 401, w.Code)
		require.NotContains(t, w.Body.String(), "secret")
	}
	require.Zero(t, calls)
	r := httptest.NewRequest(http.MethodPost, ExportPath, nil)
	r.Header.Set("Authorization", "Bearer good")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, 405, w.Code)
	require.Zero(t, calls)
	r = httptest.NewRequest(http.MethodGet, ExportPath, nil)
	r.Header.Add("Authorization", "Bearer good")
	r.Header.Add("Authorization", "Bearer good")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, 401, w.Code)
	r.Header.Set("Authorization", "Bearer good")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, 200, w.Code)
	require.Equal(t, 1, calls)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	_, err = mcpregistry.DecodeCatalogBundle(w.Body)
	require.NoError(t, err)
	handler = NewExportHandler(func(context.Context) (mcpregistry.CatalogBundle, error) {
		return mcpregistry.CatalogBundle{}, errors.New("secret")
	}, v)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), "secret")
	var b boundedBuffer
	_, err = b.Write(make([]byte, mcpregistry.CatalogBundleByteLimit+1))
	require.Error(t, err)
	require.Zero(t, b.Len())
}

func TestClient(t *testing.T) {
	t.Parallel()
	tokens := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "secret-token"})
	for _, u := range []string{"http://example.test" + ExportPath, "https://user@example.test" + ExportPath, "https://example.test" + ExportPath + "?q=x", "https://example.test" + ExportPath + "?", "https://example.test" + ExportPath + "#x", "https://example.test/other", "https://example.test/admin/%63atalog.export"} {
		_, err := NewClient(u, tokens, nil)
		require.Error(t, err)
	}
	for _, status := range []int{200, 302, 307, 403, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			requests := 0
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, ExportPath, r.URL.Path)
				assert.Equal(t, "Bearer secret-token", r.Header.Get("Authorization"))
				w.Header().Set("Location", "/secret-token")
				w.WriteHeader(status)
				if status == 200 {
					_, _ = io.WriteString(w, `{"version":1,"entries":[],"assets":{}}`)
				} else {
					_, _ = io.WriteString(w, "secret-token")
				}
			}))
			defer s.Close()
			client, err := NewClient(s.URL+ExportPath, tokens, s.Client())
			require.NoError(t, err)
			bundle, err := client.Fetch(t.Context())
			if status == 200 {
				require.NoError(t, err)
				require.Equal(t, 1, bundle.Version)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret-token")
			}
			require.Equal(t, 1, requests)
		})
	}
}

func TestTokenSourceConstructionIsLazy(t *testing.T) {
	t.Parallel()
	for _, reader := range []string{"", "reader@example.iam.gserviceaccount.com"} {
		source, err := NewGoogleTokenSource(t.Context(), "catalog-audience", reader)
		require.NoError(t, err)
		lazy, ok := source.(*lazyTokenSource)
		require.True(t, ok)
		require.Nil(t, lazy.token)
	}
	_, err := NewGoogleTokenSource(t.Context(), "", "")
	require.Error(t, err)
}

func TestClientRejectsInvalidAndOversizedResponses(t *testing.T) {
	t.Parallel()
	tokens := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "secret-token"})
	for _, body := range []string{`{"version":1} {}`, `{"unknown":"secret-token"}`, strings.Repeat(" ", mcpregistry.CatalogBundleByteLimit+1)} {
		client, err := NewClient("https://example.test"+ExportPath, tokens, &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		require.NoError(t, err)
		_, err = client.Fetch(t.Context())
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret-token")
	}
	client, err := NewClient("https://example.test"+ExportPath, tokens, &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-token") })})
	require.NoError(t, err)
	_, err = client.Fetch(t.Context())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-token")
}

type tokenSourceFunc func() (*oauth2.Token, error)

func (f tokenSourceFunc) Token() (*oauth2.Token, error) { return f() }

func TestFetchCancelsTokenAcquisition(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"discovery", "token", "waiting"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			source, err := NewGoogleTokenSource(context.Background(), "audience", "reader")
			require.NoError(t, err)
			lazy, ok := source.(*lazyTokenSource)
			require.True(t, ok)
			started := make(chan struct{})
			lazy.newSource = func(ctx context.Context, _, _ string) (oauth2.TokenSource, error) {
				close(started)
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), tokenAcquisitionTimeout)
				if phase == "discovery" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return tokenSourceFunc(func() (*oauth2.Token, error) { <-ctx.Done(); return nil, ctx.Err() }), nil
			}
			if phase == "waiting" {
				lazy.gate <- struct{}{}
				close(started)
			}
			client, err := NewClient("https://example.test"+ExportPath, source, nil)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := client.Fetch(ctx); done <- err }()
			<-started
			cancel()
			select {
			case err := <-done:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("Fetch did not honor cancellation")
			}
		})
	}
}

func TestRefreshUsesFreshRequestContext(t *testing.T) {
	t.Parallel()
	startup, stop := context.WithCancel(t.Context())
	stop()
	source, err := NewGoogleTokenSource(startup, "audience", "")
	require.NoError(t, err)
	lazy, ok := source.(*lazyTokenSource)
	require.True(t, ok)
	calls := 0
	lazy.newSource = func(ctx context.Context, _, _ string) (oauth2.TokenSource, error) {
		calls++
		require.NoError(t, ctx.Err())
		return tokenSourceFunc(func() (*oauth2.Token, error) {
			require.NoError(t, ctx.Err())
			return &oauth2.Token{AccessToken: "token", Expiry: time.Now().Add(time.Hour)}, nil
		}), nil
	}
	first, cancel := context.WithCancel(t.Context())
	token, err := lazy.TokenContext(first)
	require.NoError(t, err)
	cancel()
	token.AccessToken = "mutated"
	cached, err := lazy.TokenContext(t.Context())
	require.NoError(t, err)
	require.Equal(t, "token", cached.AccessToken)
	require.Equal(t, 1, calls)
	lazy.token.Expiry = time.Now().Add(-time.Minute)
	_, err = lazy.TokenContext(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
