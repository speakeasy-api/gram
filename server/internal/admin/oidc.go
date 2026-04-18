package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// ErrAdminDomainNotAllowed is returned when an authenticated OIDC user
// belongs to a hosted domain outside the configured admin allow list.
var ErrAdminDomainNotAllowed = errors.New("oidc account domain is not in the admin allow list")

// ErrOIDCUnauthenticated is returned when the OIDC provider refuses an access token.
var ErrOIDCUnauthenticated = errors.New("oidc provider rejected access token")

// ErrIDTokenMissing is returned when the OIDC provider's token response does not carry
// an id_token. This should not happen for the openid scope; treat it as a
// hard authentication failure.
var ErrIDTokenMissing = errors.New("oidc provider token response missing id_token")

// OIDCClientOptions holds the inputs required to drive the admin OIDC OAuth
// flow: the application's OAuth 2.0 client credentials, the redirect URL
// the OIDC provider will send the user back to after consent, and the allow
// list of hosted domains (hd) that may authenticate.
type OIDCClientOptions struct {
	HTTPClient   *guardian.HTTPClient
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AllowedHDs   []string
	Provider     *oidc.Provider
}

type OIDCClient struct {
	oauth2Config    *oauth2.Config
	allowedHDs      []string
	httpClient      *guardian.HTTPClient
	provider        *oidc.Provider
	idTokenverifier *oidc.IDTokenVerifier
}

func NewOIDCClient(cfg OIDCClientOptions) *OIDCClient {
	return &OIDCClient{
		oauth2Config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     cfg.Provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		allowedHDs:      cfg.AllowedHDs,
		httpClient:      cfg.HTTPClient,
		provider:        cfg.Provider,
		idTokenverifier: cfg.Provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}
}

// AuthCodeURL returns the OAuth consent URL for the given opaque state and PKCE
// challenge. access_type=offline + prompt=consent ensures we receive a refresh
// token for long-lived sessions.
func (g *OIDCClient) AuthCodeURL(state, pkceChallenge string) string {
	return g.oauth2Config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
		oauth2.SetAuthURLParam("code_challenge", pkceChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// Exchange converts an OAuth authorization code into an OAuth token. The
// PKCE verifier must match the challenge sent with AuthCodeURL.
func (g *OIDCClient) Exchange(ctx context.Context, code, pkceVerifier string) (*oauth2.Token, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, g.httpClient)
	tok, err := g.oauth2Config.Exchange(
		ctx,
		code,
		oauth2.SetAuthURLParam("code_verifier", pkceVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("oauth code exchange: %w", err)
	}
	return tok, nil
}

// AdminIdentity is the subset of an authenticated OIDC user we care about
// for the admin session record.
type AdminIdentity struct {
	Email       string
	Name        string
	OIDCSubject string
	HD          string
}

// VerifyIDToken validates the OIDC-issued id_token cryptographically,
// confirms the email is verified, and asserts the hosted-domain claim falls
// within the configured admin allow list.
func (g *OIDCClient) VerifyIDToken(ctx context.Context, idTokenStr string) (AdminIdentity, error) {
	payload, err := g.idTokenverifier.Verify(ctx, idTokenStr)
	if err != nil {
		return AdminIdentity{}, fmt.Errorf("verify id_token: %w", err)
	}

	type claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		HD            string `json:"hd"`
	}

	var c claims
	if err := payload.Claims(&c); err != nil {
		return AdminIdentity{}, fmt.Errorf("parse id_token claims: %w", err)
	}

	if !c.EmailVerified {
		return AdminIdentity{}, errors.New("id_token email not verified")
	}

	email := c.Email
	if email == "" {
		return AdminIdentity{}, errors.New("id_token missing email claim")
	}

	name := c.Name
	if c.Name == "" {
		return AdminIdentity{}, errors.New("id_token missing name claim")
	}

	hd := c.HD
	if !slices.Contains(g.allowedHDs, hd) {
		return AdminIdentity{}, ErrAdminDomainNotAllowed
	}

	return AdminIdentity{
		Email:       email,
		Name:        name,
		OIDCSubject: payload.Subject,
		HD:          hd,
	}, nil
}

// ExtractIDToken returns the id_token string from an OIDC token
// response.
func ExtractIDToken(tok *oauth2.Token) (string, error) {
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", ErrIDTokenMissing
	}
	return raw, nil
}

// Refresh obtains a new access token using the stored refresh token.
// Failure is treated as an authentication failure by the caller.
func (g *OIDCClient) Refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, g.httpClient)
	ts := g.oauth2Config.TokenSource(ctx, &oauth2.Token{
		RefreshToken: refreshToken,
		AccessToken:  "",
		TokenType:    "",
		Expiry:       time.Time{},
		ExpiresIn:    0,
	})
	tok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh  token: %w", err)
	}
	return tok, nil
}

// oidcUserinfo matches the OIDC userinfo response we care about.
type oidcUserinfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	HD            string `json:"hd"`
}

// Userinfo calls the OIDC provider's userinfo endpoint with the given access
// token and returns the current identity claims. A non-2xx response indicates
// the token is no longer valid (revoked, expired, account off-boarded).
func (g *OIDCClient) Userinfo(ctx context.Context, accessToken string) (AdminIdentity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.provider.UserInfoEndpoint(), nil)
	if err != nil {
		return AdminIdentity{}, fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return AdminIdentity{}, fmt.Errorf("call userinfo: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode >= 400 {
		return AdminIdentity{}, ErrOIDCUnauthenticated
	}

	var info oidcUserinfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return AdminIdentity{}, fmt.Errorf("decode userinfo: %w", err)
	}
	if !info.EmailVerified {
		return AdminIdentity{}, errors.New("userinfo email not verified")
	}
	if !slices.Contains(g.allowedHDs, info.HD) {
		return AdminIdentity{}, ErrAdminDomainNotAllowed
	}

	return AdminIdentity{
		Email:       info.Email,
		Name:        "",
		OIDCSubject: info.Sub,
		HD:          info.HD,
	}, nil
}

// NeedsRefresh returns true when the access token is expired or close to it.
// A small skew avoids a race where a token technically valid now expires
// during the downstream userinfo call.
func NeedsRefresh(expiresAt time.Time) bool {
	return time.Now().Add(60 * time.Second).After(expiresAt)
}
