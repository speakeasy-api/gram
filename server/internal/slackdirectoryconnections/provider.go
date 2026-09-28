package slackdirectoryconnections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

const directoryScopes = "users:read,users:read.email"

// TokenBundle is encrypted as one versioned object. Never log or return it.
type TokenBundle struct {
	Version      int        `json:"version"`
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	TokenType    string     `json:"token_type"`
}

type Authorization struct {
	WorkspaceID   string
	WorkspaceName string
	Scopes        []string
	Tokens        TokenBundle
}

// Provider separates the Slack protocol from connection lifecycle transactions.
type Provider interface {
	AuthorizationURL(state, workspaceID string) string
	Exchange(context.Context, string) (*Authorization, error)
}

type OAuthProvider struct {
	api          *slackapi.Client
	clientID     string
	clientSecret string
	redirectURI  string
}

func NewOAuthProvider(api *slackapi.Client, clientID, clientSecret, redirectURI string) *OAuthProvider {
	return &OAuthProvider{api: api, clientID: clientID, clientSecret: clientSecret, redirectURI: redirectURI}
}

func (p *OAuthProvider) AuthorizationURL(state, workspaceID string) string {
	q := url.Values{"client_id": {p.clientID}, "scope": {directoryScopes}, "redirect_uri": {p.redirectURI}, "state": {state}}
	if workspaceID != "" {
		q.Set("team", workspaceID)
	}
	return "https://slack.com/oauth/v2/authorize?" + q.Encode()
}

func (p *OAuthProvider) Exchange(ctx context.Context, code string) (*Authorization, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	form := url.Values{"code": {code}, "redirect_uri": {p.redirectURI}, "grant_type": {"authorization_code"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.api.BaseURL()+"/oauth.v2.access", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, providerFailure("request_invalid")
	}
	req.SetBasicAuth(p.clientID, p.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	issuedAt := time.Now()
	resp, err := p.api.HTTPClient().Do(req)
	if err != nil {
		return nil, providerFailure("transport")
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	// Provider errors can contain tokens or response bodies. Only bounded local errors leave this adapter.
	if resp.StatusCode != http.StatusOK {
		return nil, providerFailure("http_status")
	}
	var result struct {
		OK                bool   `json:"ok"`
		Error             string `json:"error"`
		AccessToken       string `json:"access_token"`
		RefreshToken      string `json:"refresh_token"`
		ExpiresIn         int64  `json:"expires_in"`
		TokenType         string `json:"token_type"`
		Scope             string `json:"scope"`
		EnterpriseInstall bool   `json:"is_enterprise_install"`
		Team              struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, providerFailure("decode")
	}
	if !result.OK {
		return nil, providerFailure(slackErrorCode(result.Error))
	}
	if result.EnterpriseInstall {
		return nil, providerFailure("enterprise_install")
	}
	if result.AccessToken == "" || result.TokenType != "bot" || result.Team.ID == "" || result.EnterpriseInstall || result.ExpiresIn < 0 {
		return nil, providerFailure("workspace_required")
	}
	scopes := strings.Split(result.Scope, ",")
	for required := range strings.SplitSeq(directoryScopes, ",") {
		if !slices.Contains(scopes, required) {
			return nil, providerFailure("scope_missing")
		}
	}
	verified, err := p.api.CallWithToken(ctx, "auth.test", nil, result.AccessToken)
	if err != nil {
		if providerErr, ok := errors.AsType[*slackapi.Error](err); ok {
			return nil, providerFailure(slackErrorCode(providerErr.Code))
		}
		return nil, providerFailure("verification_failed")
	}
	var identity struct {
		TeamID string `json:"team_id"`
		Team   string `json:"team"`
	}
	if err := json.Unmarshal(verified, &identity); err != nil || identity.TeamID != result.Team.ID {
		return nil, providerFailure("team_mismatch")
	}
	var expiresAt *time.Time
	if result.ExpiresIn > 0 {
		if result.RefreshToken == "" || result.ExpiresIn > 365*24*60*60 {
			return nil, providerFailure("rotation_invalid")
		}
		expiresAt = new(issuedAt.Add(time.Duration(result.ExpiresIn) * time.Second))
	}
	return &Authorization{WorkspaceID: identity.TeamID, WorkspaceName: identity.Team, Scopes: scopes, Tokens: TokenBundle{Version: 1, AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: expiresAt, TokenType: result.TokenType}}, nil
}

// TokenRefresher exchanges a rotating Slack refresh token for a new token bundle.
// Slack refresh tokens are single use, so callers must persist the result before using it.
type TokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (*TokenBundle, error)
}

func (p *OAuthProvider) Refresh(ctx context.Context, refreshToken string) (*TokenBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.api.BaseURL()+"/oauth.v2.access", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, providerFailure("request_invalid")
	}
	req.SetBasicAuth(p.clientID, p.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	issuedAt := time.Now()
	resp, err := p.api.HTTPClient().Do(req)
	if err != nil {
		return nil, providerFailure("transport")
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode == http.StatusTooManyRequests {
		seconds, err := strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil || seconds < 1 {
			seconds = 60
		}
		return nil, fmt.Errorf("authorize workspace: %w", &ProviderError{Code: "rate_limited", RetryAfter: time.Duration(min(seconds, 86400)) * time.Second})
	}
	if resp.StatusCode != http.StatusOK {
		return nil, providerFailure("http_status")
	}
	var result struct {
		OK           bool   `json:"ok"`
		Error        string `json:"error"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, providerFailure("decode")
	}
	if !result.OK {
		return nil, providerFailure(slackErrorCode(result.Error))
	}
	if result.AccessToken == "" || result.TokenType != "bot" || result.RefreshToken == "" || result.ExpiresIn <= 0 || result.ExpiresIn > 365*24*60*60 {
		return nil, providerFailure("rotation_invalid")
	}
	return &TokenBundle{Version: 1, AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, ExpiresAt: new(issuedAt.Add(time.Duration(result.ExpiresIn) * time.Second)), TokenType: result.TokenType}, nil
}

// ProviderError contains only an allowlisted code. Raw Slack errors may include response bodies.
type ProviderError struct {
	Code       string
	RetryAfter time.Duration
}

func (e *ProviderError) Error() string { return "Slack authorization: " + e.Code }
func providerFailure(code string) error {
	return fmt.Errorf("authorize workspace: %w", &ProviderError{Code: code, RetryAfter: 0})
}
func slackErrorCode(code string) string {
	switch code {
	case "invalid_code", "code_already_used", "invalid_client_id", "bad_client_secret", "bad_redirect_uri", "invalid_grant", "invalid_auth", "token_revoked", "token_expired", "account_inactive", "access_denied", "ratelimited", "rate_limited", "internal_error":
		return code
	default:
		return "provider_rejected"
	}
}
