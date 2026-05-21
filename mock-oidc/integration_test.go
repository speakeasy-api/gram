package mockoidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	mockoidc "github.com/speakeasy-api/gram/mock-oidc"
)

func newTestServer(t *testing.T) (*httptest.Server, *mockoidc.Config) {
	t.Helper()

	cfg := &mockoidc.Config{
		Provider: mockoidc.ProviderConfig{
			Users: []mockoidc.User{
				{
					Email:         "eng@speakeasyapi.dev",
					Name:          "Engineering User",
					HD:            "speakeasyapi.dev",
					Picture:       "https://example.com/avatar.png",
					EmailVerified: true,
				},
				{
					Email:         "external@gmail.com",
					Name:          "External User",
					EmailVerified: true,
				},
			},
			OAuthClients: []mockoidc.OAuthClient{
				{
					ClientID:     "test-client",
					ClientSecret: "test-secret",
					Name:         "Test",
					RedirectURIs: []string{"http://localhost:9999/callback"},
				},
			},
		},
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create a temporary server to capture its URL, then rebuild the provider
	// with that URL as the issuer.
	ts := httptest.NewUnstartedServer(http.NotFoundHandler())
	ts.Start()

	provider, err := mockoidc.NewProvider(cfg, logger, ts.URL, key)
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	ts.Config.Handler = mockoidc.NewServer(provider, logger).Handler()

	t.Cleanup(ts.Close)
	return ts, cfg
}

func TestDiscoveryAndIDToken(t *testing.T) {
	ts, _ := newTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, ts.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint:     provider.Endpoint(),
		RedirectURL:  "http://localhost:9999/callback",
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}

	authURL := oauth2Cfg.AuthCodeURL("xyz", oauth2.AccessTypeOnline)

	// Disable redirect-following so we can extract the code.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// GET /authorize -> challenge HTML with state_token
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET authorize: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize GET status %d: %s", resp.StatusCode, body)
	}

	stateToken := extractAttr(string(body), `name="state_token" value="`, `"`)
	if stateToken == "" {
		t.Fatalf("no state_token in challenge HTML")
	}

	// POST /authorize with chosen user
	form := url.Values{}
	form.Set("user_email", "eng@speakeasyapi.dev")
	form.Set("state_token", stateToken)
	postReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/authorize", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postResp, err := client.Do(postReq)
	if err != nil {
		t.Fatalf("POST authorize: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusFound {
		t.Fatalf("authorize POST status %d", postResp.StatusCode)
	}
	loc, err := postResp.Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect: %s", loc.String())
	}
	if loc.Query().Get("state") != "xyz" {
		t.Fatalf("state mismatch: %s", loc.Query().Get("state"))
	}

	// Exchange code -> token
	token, err := oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		t.Fatalf("token exchange: %v", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		t.Fatalf("no id_token in response")
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: "test-client"})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		t.Fatalf("verify id_token: %v", err)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		HD            string `json:"hd"`
		Picture       string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		t.Fatalf("claims: %v", err)
	}

	if claims.Email != "eng@speakeasyapi.dev" {
		t.Errorf("email = %q, want eng@speakeasyapi.dev", claims.Email)
	}
	if !claims.EmailVerified {
		t.Errorf("email_verified = false, want true")
	}
	if claims.HD != "speakeasyapi.dev" {
		t.Errorf("hd = %q, want speakeasyapi.dev", claims.HD)
	}
	if claims.Name != "Engineering User" {
		t.Errorf("name = %q", claims.Name)
	}
	if claims.Picture == "" {
		t.Errorf("picture missing")
	}

	// userinfo
	userinfo, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		t.Fatalf("userinfo: %v", err)
	}
	var ui map[string]any
	if err := userinfo.Claims(&ui); err != nil {
		t.Fatalf("userinfo claims: %v", err)
	}
	if ui["email"] != "eng@speakeasyapi.dev" {
		t.Errorf("userinfo email = %v", ui["email"])
	}
	if ui["hd"] != "speakeasyapi.dev" {
		t.Errorf("userinfo hd = %v", ui["hd"])
	}
}

func TestIDTokenOmitsHDForExternalUser(t *testing.T) {
	ts, _ := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rawIDToken := loginAndExchange(t, ctx, ts.URL, "external@gmail.com")

	parts := strings.Split(rawIDToken, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed jwt")
	}
	payload := decodeJWTPart(t, parts[1])
	if _, exists := payload["hd"]; exists {
		t.Errorf("hd should not be present for non-hd user; got %v", payload["hd"])
	}
}

func TestRejectsUnknownClient(t *testing.T) {
	ts, _ := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	u := ts.URL + "/authorize?client_id=nope&response_type=code&redirect_uri=http://localhost:9999/callback"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func loginAndExchange(t *testing.T, ctx context.Context, baseURL, email string) string {
	t.Helper()
	authURL := baseURL + "/authorize?" + url.Values{
		"client_id":     {"test-client"},
		"response_type": {"code"},
		"redirect_uri":  {"http://localhost:9999/callback"},
		"scope":         {"openid email profile"},
		"state":         {"abc"},
	}.Encode()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET authorize: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	stateToken := extractAttr(string(body), `name="state_token" value="`, `"`)

	form := url.Values{"user_email": {email}, "state_token": {stateToken}}
	postReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/authorize", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postResp, err := client.Do(postReq)
	if err != nil {
		t.Fatalf("POST authorize: %v", err)
	}
	postResp.Body.Close()
	loc, _ := postResp.Location()
	code := loc.Query().Get("code")

	tokForm := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {"http://localhost:9999/callback"},
	}
	tokReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/token", strings.NewReader(tokForm.Encode()))
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokReq.SetBasicAuth("test-client", "test-secret")
	tokResp, err := http.DefaultClient.Do(tokReq)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer tokResp.Body.Close()
	if tokResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(tokResp.Body)
		t.Fatalf("token status %d: %s", tokResp.StatusCode, b)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(tokResp.Body).Decode(&tok); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	return tok.IDToken
}

func extractAttr(s, prefix, suffix string) string {
	_, after, ok := strings.Cut(s, prefix)
	if !ok {
		return ""
	}
	rest := after
	before, _, ok := strings.Cut(rest, suffix)
	if !ok {
		return ""
	}
	return before
}

func decodeJWTPart(t *testing.T, part string) map[string]any {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		t.Fatalf("decode jwt part: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal jwt: %v", err)
	}
	return m
}
